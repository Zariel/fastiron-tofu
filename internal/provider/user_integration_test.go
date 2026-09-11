package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type (
	userEntry struct {
		privilege       int64
		password, extra string
	}
	userSwitch struct {
		running, startup map[string]userEntry
		failWrite        bool
		protected        bool
		writes           int
	}
)

func userConfig(users map[string]userEntry, protected bool) string {
	text := ""
	if protected {
		text += "service local-user-protection\n"
	}
	for _, name := range slices.Sorted(maps.Keys(users)) {
		u := users[name]
		text += fmt.Sprintf("username %s privilege %d", name, u.privilege)
		if u.password != "" {
			text += " password opaque-user-hash"
		}
		text += "\n"
		if u.extra != "" {
			text += "username " + name + " " + u.extra + "\n"
		}
	}
	return text
}

func (s *userSwitch) rest(w http.ResponseWriter, r *http.Request) {
	root := "/restconf/data/system/aaa/authentication/users"
	if r.Method == "GET" && r.URL.Path == root {
		users := []any{}
		for _, name := range slices.Sorted(maps.Keys(s.running)) {
			u := s.running[name]
			users = append(users, map[string]any{"username": name, "config": map[string]any{"username": name, "password": "$6$opaque-response", "icx-openconfig-aaa-aug:privilege": u.privilege}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-system:users": map[string]any{"user": users}})
		return
	}
	if r.Method == "DELETE" {
		for name := range s.running {
			if r.URL.Path == root+"/user="+name {
				delete(s.running, name)
				s.writes++
				w.WriteHeader(204)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	if r.Method != "PATCH" || r.URL.Path != root {
		http.Error(w, "unsupported", 405)
		return
	}
	var body struct {
		Users struct {
			User []struct {
				Username string `json:"username"`
				Config   struct {
					Username  string  `json:"username"`
					Password  *string `json:"password"`
					Privilege *int64  `json:"icx-openconfig-aaa-aug:privilege"`
				} `json:"config"`
			} `json:"user"`
		} `json:"users"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Users.User) != 1 {
		http.Error(w, "invalid", 400)
		return
	}
	entry := body.Users.User[0]
	if entry.Config.Username != entry.Username {
		http.Error(w, "identity mismatch", 400)
		return
	}
	u := s.running[entry.Username]
	if entry.Config.Privilege != nil {
		u.privilege = *entry.Config.Privilege
	}
	if entry.Config.Password != nil {
		u.password = *entry.Config.Password
	}
	s.running[entry.Username] = u
	s.writes++
	if s.failWrite {
		http.Error(w, "response failed after password mutation", 500)
		return
	}
	w.WriteHeader(204)
}

func TestOpenTofuUser(t *testing.T) {
	s := newSwitch(t)
	neighbor := userEntry{privilege: 0, password: "neighbor-password"}
	s.userAccounts = &userSwitch{running: map[string]userEntry{"automation": neighbor}, startup: map[string]userEntry{"automation": neighbor}}
	write, run, base := tofuFixture(t, s)
	base = strings.Replace(base, `provider "fastiron" {`, `provider "fastiron" {`+"\n allow_aaa_changes=true", 1)
	config := func(name string, privilege int, password string) {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_aaa_user\" \"test\" {\nusername=%q\nprivilege=%d\npassword=%q\n}\n", name, privilege, password))
	}
	check := func(name string, privilege int64, password string) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.userAccounts.running[name] != (userEntry{privilege: privilege, password: password}) || !maps.Equal(s.userAccounts.running, s.userAccounts.startup) {
			t.Fatal("local user configuration/persistence differs")
		}
		if s.userAccounts.running["automation"] != neighbor {
			t.Fatal("neighboring account changed")
		}
	}
	config("reader", 5, "test-password-a")
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check("reader", 5, "test-password-a")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	if strings.Contains(run(0, "state", "pull"), "opaque-response") {
		t.Fatal("returned password hash reached state")
	}
	run(0, "state", "rm", "fastiron_aaa_user.test")
	run(0, "import", "-no-color", "fastiron_aaa_user.test", "username reader")
	run(0, "apply", "-auto-approve", "-no-color")
	check("reader", 5, "test-password-a")
	config("reader", 4, "test-password-b")
	s.mu.Lock()
	s.userAccounts.failWrite = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.userAccounts.failWrite = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check("reader", 4, "test-password-b")
	s.mu.Lock()
	u := s.userAccounts.running["reader"]
	u.privilege = 5
	s.userAccounts.running["reader"] = u
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check("reader", 4, "test-password-b")
	s.mu.Lock()
	u = s.userAccounts.running["reader"]
	u.extra = "expires 30"
	s.userAccounts.running["reader"] = u
	writes := s.userAccounts.writes
	s.mu.Unlock()
	config("reader", 5, "test-password-c")
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	if s.userAccounts.writes != writes {
		s.mu.Unlock()
		t.Fatal("account restriction was overwritten")
	}
	u.extra = ""
	s.userAccounts.running["reader"] = u
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check("reader", 5, "test-password-c")
	config("renamed", 5, "test-password-c")
	run(0, "plan", "-out=rename.plan", "-no-color")
	var plan struct {
		Changes []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(run(0, "show", "-json", "rename.plan")), &plan); err != nil {
		t.Fatal(err)
	}
	replaced := false
	for _, c := range plan.Changes {
		if c.Address == "fastiron_aaa_user.test" {
			replaced = slices.Equal(c.Change.Actions, []string{"delete", "create"})
		}
	}
	if !replaced {
		t.Fatal("username change was not an explicit replacement")
	}
	run(0, "apply", "-auto-approve", "-no-color", "rename.plan")
	check("renamed", 5, "test-password-c")
	s.mu.Lock()
	s.failSave = true
	s.mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.userAccounts.protected = true
	s.mu.Unlock()
	// No user mutation remains after failed persistence; protection must not block the save retry.
	run(0, "destroy", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.userAccounts.protected = false
	s.mu.Unlock()
	config("automation", 0, "replacement-password")
	if output := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(output, "transport account") {
		t.Fatal("provider transport account was not protected")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.userAccounts.running) != 1 || s.userAccounts.running["automation"] != neighbor || !maps.Equal(s.userAccounts.running, s.userAccounts.startup) {
		t.Fatal("cleanup changed neighboring accounts")
	}
}
