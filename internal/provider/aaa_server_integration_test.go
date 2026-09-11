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
	aaaEntry struct {
		kind, address, purpose, key, extra string
		auth, acct                         int64
	}
	aaaSwitch struct {
		running, startup map[string]aaaEntry
		failWrite        bool
		reads, writes    int
	}
)

func aaaConfiguration(entries map[string]aaaEntry) string {
	text := "radius-server retransmit 4\naaa authentication login default local\n"
	for _, id := range slices.Sorted(maps.Keys(entries)) {
		e := entries[id]
		text += fmt.Sprintf("%s-server host %s  auth-port %d", e.kind, e.address, e.auth)
		if e.kind == "radius" {
			text += fmt.Sprintf(" acct-port %d", e.acct)
		}
		text += " " + e.purpose
		if e.key != "" {
			text += " key 2 opaque-native"
		}
		if e.extra != "" {
			text += " " + e.extra
		}
		text += "\n"
	}
	return text
}

func (s *aaaSwitch) rest(w http.ResponseWriter, r *http.Request) {
	root := "/restconf/data/system/aaa/server-groups"
	if r.Method == "GET" && r.URL.Path == root {
		s.reads++
		groups := []any{}
		for _, kind := range []string{"radius", "tacacs"} {
			servers := []any{}
			for _, id := range slices.Sorted(maps.Keys(s.running)) {
				e := s.running[id]
				if e.kind != kind {
					continue
				}
				c := map[string]any{"secret-key": fmt.Sprintf("opaque-response-%d", s.reads), "icx-openconfig-aaa-aug:purpose": e.purpose}
				if kind == "radius" {
					c["auth-port"], c["acct-port"] = e.auth, e.acct
				} else {
					c["port"] = e.auth
				}
				servers = append(servers, map[string]any{"address": e.address, "config": map[string]any{"address": e.address}, kind: map[string]any{"config": c}})
			}
			group := kind + "-default-group"
			groups = append(groups, map[string]any{"name": group, "config": map[string]any{"name": group, "type": strings.ToUpper(kind)}, "servers": map[string]any{"server": servers}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-system:server-groups": map[string]any{"server-group": groups}})
		return
	}
	if r.Method == "DELETE" {
		for id, e := range s.running {
			if r.URL.Path == root+"/server-group="+e.kind+"-default-group/servers/server="+e.address {
				delete(s.running, id)
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
		Groups struct {
			Group []struct {
				Name    string `json:"name"`
				Servers struct {
					Server []struct {
						Address string `json:"address"`
						Radius  struct {
							Config map[string]any `json:"config"`
						} `json:"radius"`
						Tacacs struct {
							Config map[string]any `json:"config"`
						} `json:"tacacs"`
					} `json:"server"`
				} `json:"servers"`
			} `json:"server-group"`
		} `json:"server-groups"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Groups.Group) != 1 || len(body.Groups.Group[0].Servers.Server) != 1 {
		http.Error(w, "invalid", 400)
		return
	}
	group := body.Groups.Group[0]
	server := group.Servers.Server[0]
	kind := strings.TrimSuffix(group.Name, "-default-group")
	e := aaaEntry{kind: kind, address: server.Address, auth: 49, purpose: "default"}
	c := server.Tacacs.Config
	if kind == "radius" {
		c = server.Radius.Config
		e.auth, e.acct = 1812, 1813
	}
	if v, ok := c["auth-port"].(float64); ok {
		e.auth = int64(v)
	}
	if v, ok := c["port"].(float64); ok {
		e.auth = int64(v)
	}
	if v, ok := c["acct-port"].(float64); ok {
		e.acct = int64(v)
	}
	if v, ok := c["icx-openconfig-aaa-aug:purpose"].(string); ok {
		e.purpose = v
	}
	e.key, _ = c["secret-key"].(string)
	s.running[kind+"|"+e.address] = e
	s.writes++
	if s.failWrite {
		http.Error(w, "response failed after applying key", 500)
		return
	}
	w.WriteHeader(204)
}

func TestOpenTofuAAAServer(t *testing.T) {
	for _, kind := range []string{"radius", "tacacs"} {
		t.Run(kind, func(t *testing.T) {
			s := newSwitch(t)
			neighbor := aaaEntry{kind: kind, address: "192.0.2.54", auth: 49, purpose: "default", key: "neighbor-key"}
			if kind == "radius" {
				neighbor.auth, neighbor.acct = 1812, 1813
			}
			s.aaa = &aaaSwitch{running: map[string]aaaEntry{kind + "|192.0.2.54": neighbor}, startup: map[string]aaaEntry{kind + "|192.0.2.54": neighbor}}
			write, run, base := tofuFixture(t, s)
			base = strings.Replace(base, `provider "fastiron" {`, `provider "fastiron" {`+"\n allow_aaa_changes=true", 1)
			address := "fastiron_aaa_" + kind + "_server.test"
			config := func(port int, key string) {
				attrs := fmt.Sprintf("address=\"192.0.2.53\"\nauth_port=%d\npurpose=\"accounting-only\"\n", port)
				if kind == "radius" {
					attrs += fmt.Sprintf("acct_port=%d\n", port+1)
				}
				if key != "" {
					attrs += fmt.Sprintf("secret=%q\n", key)
				}
				write("main.tf", base+fmt.Sprintf("resource %q \"test\" {\n%s}\n", "fastiron_aaa_"+kind+"_server", attrs))
			}
			check := func(port int64, key string) {
				t.Helper()
				s.mu.Lock()
				defer s.mu.Unlock()
				e := s.aaa.running[kind+"|192.0.2.53"]
				if e.auth != port || e.key != key || e.purpose != "accounting-only" || kind == "radius" && e.acct != port+1 {
					t.Fatal("AAA state differs from desired metadata/key")
				}
				if !maps.Equal(s.aaa.running, s.aaa.startup) {
					t.Fatal("AAA startup differs")
				}
				if s.aaa.running[kind+"|192.0.2.54"] != neighbor {
					t.Fatal("AAA neighbor changed")
				}
			}
			config(1912, "test-key-a")
			run(0, "init", "-no-color")
			run(0, "apply", "-auto-approve", "-no-color")
			check(1912, "test-key-a")
			run(0, "plan", "-detailed-exitcode", "-no-color")
			allowed := base
			base = strings.Replace(base, "allow_aaa_changes=true", "allow_aaa_changes=false", 1)
			config(1912, "test-key-a")
			if output := run(1, "plan", "-no-color"); !strings.Contains(output, "allow_aaa_changes") {
				t.Fatal("plan did not explain AAA opt-in")
			}
			base = allowed
			config(1912, "test-key-a")
			if strings.Contains(run(0, "state", "pull"), "opaque-response-") {
				t.Fatal("returned secret material reached state")
			}
			run(0, "state", "rm", address)
			run(0, "import", "-no-color", address, kind+"-server host 192.0.2.53")
			run(0, "apply", "-auto-approve", "-no-color")
			check(1912, "test-key-a")
			config(2112, "test-key-b")
			s.mu.Lock()
			s.aaa.failWrite = true
			s.mu.Unlock()
			run(1, "apply", "-auto-approve", "-no-color")
			s.mu.Lock()
			s.aaa.failWrite = false
			s.mu.Unlock()
			run(0, "apply", "-auto-approve", "-no-color")
			check(2112, "test-key-b")
			run(0, "plan", "-detailed-exitcode", "-no-color")
			s.mu.Lock()
			drift := s.aaa.running[kind+"|192.0.2.53"]
			drift.auth = 2999
			s.aaa.running[kind+"|192.0.2.53"] = drift
			s.mu.Unlock()
			run(2, "plan", "-detailed-exitcode", "-no-color")
			run(0, "apply", "-auto-approve", "-no-color")
			check(2112, "test-key-b")
			if kind == "radius" {
				s.mu.Lock()
				guarded := s.aaa.running[kind+"|192.0.2.53"]
				guarded.extra = "dot1x"
				s.aaa.running[kind+"|192.0.2.53"] = guarded
				writes := s.aaa.writes
				s.mu.Unlock()
				config(2512, "test-key-b")
				run(1, "apply", "-auto-approve", "-no-color")
				s.mu.Lock()
				if s.aaa.writes != writes {
					s.mu.Unlock()
					t.Fatal("native-only server settings were overwritten")
				}
				guarded.extra = ""
				s.aaa.running[kind+"|192.0.2.53"] = guarded
				s.mu.Unlock()
				config(2112, "test-key-b")
				run(0, "apply", "-auto-approve", "-no-color")
				check(2112, "test-key-b")
			}
			if kind == "radius" {
				config(2112, "")
				run(0, "plan", "-out=replace.plan", "-no-color")
				var plan struct {
					Changes []struct {
						Address string `json:"address"`
						Change  struct {
							Actions []string `json:"actions"`
						} `json:"change"`
					} `json:"resource_changes"`
				}
				if err := json.Unmarshal([]byte(run(0, "show", "-json", "replace.plan")), &plan); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, c := range plan.Changes {
					if c.Address == address {
						found = slices.Equal(c.Change.Actions, []string{"delete", "create"})
					}
				}
				if !found {
					t.Fatal("key removal was not planned as replacement")
				}
				run(0, "apply", "-auto-approve", "-no-color", "replace.plan")
				check(2112, "")
			}
			s.mu.Lock()
			s.failSave = true
			s.mu.Unlock()
			run(1, "destroy", "-auto-approve", "-no-color")
			s.mu.Lock()
			s.failSave = false
			s.mu.Unlock()
			run(0, "destroy", "-auto-approve", "-no-color")
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.aaa.running) != 1 || !maps.Equal(s.aaa.running, s.aaa.startup) {
				t.Fatal("AAA deletion/persistence did not converge")
			}
		})
	}
}
