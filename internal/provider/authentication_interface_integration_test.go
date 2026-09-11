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
	authenticationPort struct {
		dot1x, mac bool
		mode       string
	}
	authenticationSwitch struct {
		extra                             string
		running, startup                  map[string]authenticationPort
		projected                         map[string]bool
		failPatch, failClear, ignorePatch bool
	}
)

func authenticationConfig(ports map[string]authenticationPort, extra string) string {
	text := "vlan 1 name DEFAULT-VLAN by port\n"
	for _, name := range slices.Sorted(maps.Keys(ports)) {
		p := ports[name]
		if p.dot1x || p.mac {
			text += " no untagged " + name + "\n"
		}
	}
	text += "!\nauthentication\n auth-default-vlan 3055\n dot1x enable\n mac-authentication enable\n"
	for _, name := range slices.Sorted(maps.Keys(ports)) {
		p := ports[name]
		if p.dot1x {
			text += " dot1x enable " + name + "\n"
		}
		if p.mac {
			text += " mac-authentication enable " + name + "\n"
		}
		if p.mode != "" && p.mode != "force-authorized" {
			text += " dot1x port-control " + p.mode + " " + name + "\n"
		}
	}
	return text + extra + "!\n"
}

func (s *authenticationSwitch) rest(w http.ResponseWriter, r *http.Request) {
	endpoint := strings.TrimPrefix(r.URL.Path, "/restconf/data/authentication/config/")
	if r.Method == "DELETE" {
		leaf, name, ok := strings.Cut(endpoint, "=")
		if !ok {
			http.Error(w, "broad deletion forbidden", 400)
			return
		}
		p := s.running[name]
		switch {
		case leaf == "dot1x/ethernet":
			p.dot1x = false
			p.mode = "force-authorized"
		case leaf == "mac-authentication/ethernet":
			p.mac = false
		case strings.HasPrefix(leaf, "dot1x/port-control/"):
			delete(s.projected, endpoint)
			p.mode = "force-authorized"
		default:
			http.Error(w, "unknown leaf", 400)
			return
		}
		s.running[name] = p
		if s.failClear {
			s.failClear = false
			http.Error(w, "ambiguous clear", 500)
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method != "PATCH" {
		http.Error(w, "unsupported method", 405)
		return
	}
	var body map[string]map[string]string
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "bad payload", 400)
		return
	}
	for family, values := range body {
		for field, name := range values {
			p := s.running[name]
			if !s.ignorePatch {
				switch family {
				case "dot1x":
					p.dot1x = true
				case "mac-authentication":
					p.mac = true
				case "port-control":
					key := "dot1x/port-control/" + field + "=" + name
					// FastIron can acknowledge a retained REST mode without reapplying it.
					if !s.projected[key] {
						p.mode = field
					}
					s.projected[key] = true
				default:
					http.Error(w, "unknown family", 400)
					return
				}
			}
			s.running[name] = p
		}
	}
	if s.failPatch {
		s.failPatch = false
		http.Error(w, "ambiguous patch", 500)
		return
	}
	w.WriteHeader(204)
}

func TestOpenTofuAuthenticationInterface(t *testing.T) {
	s := newSwitch(t)
	neighbor := authenticationPort{true, true, "auto"}
	s.auth = &authenticationSwitch{running: map[string]authenticationPort{"ethernet 1/1/3": neighbor}, startup: map[string]authenticationPort{"ethernet 1/1/3": neighbor}, projected: map[string]bool{}}
	write, run, base := tofuFixture(t, s)
	base = strings.Replace(base, `provider "fastiron" {`, `provider "fastiron" {
 allow_aaa_changes = true`, 1)
	config := func(mode string, enabled bool) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_authentication_interface" "test" {
 interface = "ethernet 1/1/2"
 dot1x_enabled = %t
 mac_authentication_enabled = %t
 port_control = %q
}
`, enabled, enabled, mode))
	}
	check := func(want authenticationPort) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, ports := range []map[string]authenticationPort{s.auth.running, s.auth.startup} {
			if ports["ethernet 1/1/2"] != want || ports["ethernet 1/1/3"] != neighbor {
				t.Fatalf("authentication state=%v", ports)
			}
		}
		if s.ethernet["description"] != "manual port" || s.ethernet["enabled"] != true {
			t.Fatal("changed base Ethernet configuration")
		}
	}
	config("force-unauthorized", true)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{true, true, "force-unauthorized"})
	run(0, "state", "rm", "fastiron_authentication_interface.test")
	run(0, "import", "fastiron_authentication_interface.test", "ethernet 1/1/2")
	for _, mode := range []string{"force-authorized", "force-unauthorized", "auto", "force-unauthorized"} {
		config(mode, true)
		run(0, "apply", "-auto-approve", "-no-color")
		check(authenticationPort{true, true, mode})
		run(0, "plan", "-detailed-exitcode", "-no-color")
	}
	s.mu.Lock()
	s.auth.running["ethernet 1/1/2"] = authenticationPort{false, true, "auto"}
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{true, true, "force-unauthorized"})

	for _, action := range []string{"auth-fail-action restricted-vlan", "auth-timeout-action success"} {
		s.mu.Lock()
		s.auth.extra = " " + action + "\n"
		s.mu.Unlock()
		config("force-authorized", false)
		if output := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(output, "require reapplying") {
			t.Fatalf("missing global action guard: %s", output)
		}
		check(authenticationPort{true, true, "force-unauthorized"})
	}
	s.mu.Lock()
	s.auth.extra = ""
	s.mu.Unlock()

	// An interrupted clear leaves an intermediate mode that must be reconciled.
	s.mu.Lock()
	s.auth.failClear = true
	s.mu.Unlock()
	config("auto", true)
	run(1, "apply", "-auto-approve", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{true, true, "auto"})
	s.mu.Lock()
	s.auth.failPatch = true
	s.mu.Unlock()
	config("force-unauthorized", true)
	run(1, "apply", "-auto-approve", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{true, true, "force-unauthorized"})

	s.mu.Lock()
	s.auth.ignorePatch = true
	s.mu.Unlock()
	config("auto", true)
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.auth.ignorePatch = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{true, true, "auto"})

	config("auto", false)
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "requires dot1x_enabled") {
		t.Fatalf("missing dot1x prerequisite: %s", output)
	}
	check(authenticationPort{true, true, "auto"})
	config("force-authorized", false)
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{false, false, "force-authorized"})
	write("main.tf", base+`resource "fastiron_authentication_interface" "test" {
 interface = "ethernet 1/1/2"
 mac_authentication_enabled = true
}
`)
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{false, true, "force-authorized"})
	config("auto", true)
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{true, true, "auto"})

	write("main.tf", base)
	s.mu.Lock()
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{false, false, "force-authorized"})
	config("force-unauthorized", true)
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{true, true, "force-unauthorized"})
	write("main.tf", strings.Replace(base, "allow_aaa_changes = true", "allow_aaa_changes = false", 1))
	run(1, "plan", "-no-color")
	write("main.tf", base)
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{false, false, "force-authorized"})

	config("auto", true)
	run(0, "apply", "-auto-approve", "-no-color")
	write("main.tf", base+`resource "fastiron_authentication_interface" "test" {
 interface = "ethernet 1/1/4"
 dot1x_enabled = true
 port_control = "auto"
}
`)
	run(0, "apply", "-auto-approve", "-no-color")
	check(authenticationPort{false, false, "force-authorized"})
	s.mu.Lock()
	if s.auth.running["ethernet 1/1/4"] != (authenticationPort{true, false, "auto"}) || s.auth.startup["ethernet 1/1/4"] != s.auth.running["ethernet 1/1/4"] {
		t.Error("replacement did not configure and save the new port")
	}
	s.mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")
}
