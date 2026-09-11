package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type policySettings struct {
	login   []string
	dot1x   string
	enabled bool
	ignored []string
}

func (p policySettings) native() string {
	text := "aaa authentication login default " + strings.Join(p.login, " ") + "\n"
	if p.dot1x != "" {
		text += "aaa authentication dot1x default " + p.dot1x + "\n"
	}
	if p.enabled {
		text += "aaa authorization coa enable\n"
	}
	if len(p.ignored) > 0 {
		text += "aaa authorization coa ignore " + strings.Join(p.ignored, " ") + "\n"
	}
	return text
}

type policySwitch struct {
	failDot1XCreate bool
	dot1xDeleted    bool

	running, startup policySettings
	projectionOnly   bool
	projected        []string
	failLogin        bool
	extra            string
	writes           int
}

func (s *policySwitch) rest(w http.ResponseWriter, r *http.Request) {
	root := "/restconf/data/system/aaa"
	if r.Method == "GET" && r.URL.Path == root {
		ignored := s.running.ignored
		if s.projectionOnly && s.projected != nil {
			ignored = s.projected
		}
		flags := map[string]bool{}
		for _, action := range []string{"disable-port", "dm-request", "flip-port", "modify-acl", "reauth-host"} {
			flags[action] = slices.Contains(ignored, action)
		}
		mode := s.running.dot1x
		if mode == "" {
			mode = "none"
		}
		response := map[string]any{"openconfig-system:aaa": map[string]any{"authentication": map[string]any{"icx-openconfig-aaa-aug:login": map[string]any{"default": s.running.login}, "icx-openconfig-aaa-aug:dot1x": map[string]string{"default": mode}}, "authorization": map[string]any{"icx-openconfig-aaa-aug:coa": map[string]any{"enable": s.running.enabled, "ignore": flags}}}}
		if s.dot1xDeleted && s.running.dot1x == "" {
			delete(response["openconfig-system:aaa"].(map[string]any)["authentication"].(map[string]any), "icx-openconfig-aaa-aug:dot1x")
		}
		json.NewEncoder(w).Encode(response)
		return
	}
	if r.Method == "DELETE" && r.URL.Path == root+"/authentication/dot1x" {
		if s.dot1xDeleted && s.running.dot1x == "" {
			http.NotFound(w, r)
			return
		}
		s.running.dot1x = ""
		s.dot1xDeleted = true
		s.writes++
		w.WriteHeader(204)
		return
	}
	var body struct {
		Login struct {
			Default []string `json:"default"`
		} `json:"login"`
		CoA struct {
			Enable bool `json:"enable"`
		} `json:"coa"`
		Ignore map[string]bool `json:"ignore"`
		Dot1X  struct {
			Default string `json:"default"`
		} `json:"icx-openconfig-aaa-aug:dot1x"`
		Authentication struct {
			Dot1X struct {
				Default string `json:"default"`
			} `json:"icx-openconfig-aaa-aug:dot1x"`
		} `json:"authentication"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	switch {
	case r.Method == "PUT" && r.URL.Path == root+"/authentication/login":
		s.running.login = body.Login.Default
	case r.Method == "PATCH" && r.URL.Path == root+"/authorization/coa":
		s.running.enabled = body.CoA.Enable
	case r.Method == "PATCH" && r.URL.Path == root+"/authorization/coa/ignore":
		ignored := []string{}
		for action, value := range body.Ignore {
			if value {
				ignored = append(ignored, action)
			}
		}
		slices.Sort(ignored)
		if s.projectionOnly {
			s.projected = ignored
		} else {
			s.running.ignored = ignored
		}
	case r.Method == "PATCH" && r.URL.Path == root+"/authentication":
		if !(s.running.dot1x == "" && body.Authentication.Dot1X.Default == "none" && !s.dot1xDeleted) {
			s.running.dot1x = body.Authentication.Dot1X.Default
			s.dot1xDeleted = false
		}
	case r.Method == "POST" && r.URL.Path == root+"/authentication":
		if s.failDot1XCreate {
			http.Error(w, "creation failed after clearing the implicit default", 500)
			return
		}
		if s.dot1xDeleted {
			s.running.dot1x = body.Dot1X.Default
			s.dot1xDeleted = false
		}
	default:
		http.Error(w, "unsupported operation", 405)
		return
	}
	s.writes++
	if s.failLogin && r.URL.Path == root+"/authentication/login" {
		http.Error(w, "response failed after login mutation", 500)
		return
	}
	w.WriteHeader(204)
}

func TestOpenTofuAAAConfiguration(t *testing.T) {
	s := newSwitch(t)
	defaults := policySettings{login: []string{"local"}}
	s.policy = &policySwitch{running: defaults, startup: defaults}
	write, run, base := tofuFixture(t, s)
	base = strings.Replace(base, `provider "fastiron" {`, `provider "fastiron" {`+"\n allow_aaa_changes=true", 1)
	config := func(login, dot1x string, enabled bool, ignored string) {
		text := fmt.Sprintf("resource \"fastiron_aaa\" \"test\" {\n login_methods=%s\n coa_enabled=%t\n coa_ignore=%s\n", login, enabled, ignored)
		if dot1x != "" {
			text += fmt.Sprintf("dot1x_default=%q\n", dot1x)
		}
		write("main.tf", base+text+"}\n")
	}
	check := func(want string) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.policy.running.native() != want || s.policy.startup.native() != want {
			t.Fatalf("AAA configuration/persistence differs: %s", s.policy.running.native())
		}
	}
	config(`["local","radius"]`, "radius", true, `["modify-acl","dm-request"]`)
	run(0, "init", "-no-color")
	write("main.tf", strings.Replace(base, "allow_aaa_changes=true", "allow_aaa_changes=false", 1)+`resource "fastiron_aaa" "test" { login_methods=["local"] }`)
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "AAA changes disabled") {
		t.Fatal("AAA policy write opt-in was not enforced during planning")
	}

	write("main.tf", base+`resource "fastiron_aaa" "test" {
 login_methods=["local"]
 dot1x_default=""
 }`)
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "omit it to remove") {
		t.Fatal("explicit empty dot1x policy was accepted")
	}
	config(`["local"]`, "none", false, `[]`)
	s.mu.Lock()
	s.policy.failDot1XCreate = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.policy.failDot1XCreate = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check("aaa authentication login default local\naaa authentication dot1x default none\n")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	config(`["local","radius"]`, "radius", true, `["modify-acl","dm-request"]`)

	run(0, "apply", "-auto-approve", "-no-color")
	check("aaa authentication login default local radius\naaa authentication dot1x default radius\naaa authorization coa enable\naaa authorization coa ignore dm-request modify-acl\n")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_aaa.test")
	run(0, "import", "-no-color", "fastiron_aaa.test", "aaa")
	run(0, "apply", "-auto-approve", "-no-color")

	config(`["radius","local"]`, "none", false, `["flip-port"]`)
	s.mu.Lock()
	s.policy.failLogin = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.policy.failLogin = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check("aaa authentication login default radius local\naaa authentication dot1x default none\naaa authorization coa ignore flip-port\n")

	config(`["radius","local"]`, "", false, `["flip-port"]`)
	run(0, "apply", "-auto-approve", "-no-color")
	check("aaa authentication login default radius local\naaa authorization coa ignore flip-port\n")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	s.mu.Lock()
	s.policy.running.dot1x = "none"
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check("aaa authentication login default radius local\naaa authorization coa ignore flip-port\n")

	config(`["local"]`, "radius", true, `["dm-request"]`)
	s.mu.Lock()
	s.policy.projectionOnly = true
	s.mu.Unlock()
	if output := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(output, "disagree") {
		t.Fatal("projection-only write was accepted")
	}
	s.mu.Lock()
	if s.policy.running.enabled || s.policy.running.dot1x != "" || !slices.Equal(s.policy.running.login, []string{"radius", "local"}) {
		s.mu.Unlock()
		t.Fatal("dependent writes continued after projection mismatch")
	}
	s.policy.projectionOnly = false
	s.policy.projected = nil
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check("aaa authentication login default local\naaa authentication dot1x default radius\naaa authorization coa enable\naaa authorization coa ignore dm-request\n")

	s.mu.Lock()
	s.policy.extra = "aaa authentication login privilege-mode\n"
	writes := s.policy.writes
	s.mu.Unlock()
	config(`["local"]`, "none", false, `[]`)
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	if s.policy.writes != writes {
		s.mu.Unlock()
		t.Fatal("native-only options were overwritten")
	}
	s.policy.extra = ""
	s.failSave = true
	s.mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	check("aaa authentication login default local\n")
}
