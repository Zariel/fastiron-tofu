package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type lagPort struct {
	enabled   bool
	aggregate string
}

type lagSwitch struct {
	names, modes  map[string]string
	ports         map[string]lagPort
	child         bool
	deleteMissing bool
}

func (s *lagSwitch) configuration() string {
	var text strings.Builder
	names := []string{}
	for name := range s.names {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		mode := "dynamic"
		if s.modes[name] == "STATIC" {
			mode = "static"
		}
		fmt.Fprintf(&text, "lag %s %s id %s\n", s.names[name], mode, strings.TrimPrefix(name, "lag "))
		var members []string
		for _, port := range []string{"ethernet 1/1/7", "ethernet 1/1/8", "ethernet 1/1/9"} {
			if s.ports[port].aggregate == name {
				members = append(members, strings.TrimPrefix(port, "ethernet "))
			}
		}
		if len(members) > 0 {
			fmt.Fprintf(&text, " ports ethe %s\n", strings.Join(members, " ethe "))
		}
		text.WriteString("!\n")
	}
	if s.child {
		text.WriteString("interface lag 53\n ip address 192.0.2.1 255.255.255.0\n!\n")
	}
	for _, name := range []string{"ethernet 1/1/7", "ethernet 1/1/8", "ethernet 1/1/9"} {
		fmt.Fprintf(&text, "interface %s\n port-name preserved\n", name)
		if !s.ports[name].enabled {
			text.WriteString(" disable\n")
		}
		text.WriteString("!\n")
	}
	return text.String()
}

// These endpoints model observed native side effects, including disabling a
// detached member and refusing to change an existing aggregate's mode.
func (s *lagSwitch) rest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/restconf/data/interfaces")
	if r.Method == "GET" && path == "" {
		entries := []any{}
		for name, port := range s.ports {
			config := map[string]any{}
			if port.aggregate != "" {
				config["openconfig-if-aggregate:aggregate-id"] = port.aggregate
			}
			entries = append(entries, map[string]any{"name": name, "config": map[string]any{"name": name, "type": "iana-if-type:ethernetCsmacd", "description": "preserved", "enabled": port.enabled}, "openconfig-if-ethernet:ethernet": map[string]any{"config": config}})
		}
		for name, label := range s.names {
			entries = append(entries, map[string]any{"name": name, "config": map[string]any{"name": name, "type": "iana-if-type:ieee8023adLag"}, "openconfig-if-aggregate:aggregation": map[string]any{"config": map[string]any{"lag-type": s.modes[name], "openconfig-if-aggregate-aug:lag-name": label}}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-interfaces:interfaces": map[string]any{"interface": entries}})
		return
	}
	if r.Method == "GET" && strings.HasSuffix(path, "/switched-vlan") {
		json.NewEncoder(w).Encode(map[string]any{"openconfig-vlan:switched-vlan": map[string]any{"config": map[string]any{"access-vlan": 1}}})
		return
	}
	if path == "" && (r.Method == "POST" || r.Method == "PATCH") {
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		if r.Method == "PATCH" {
			if json.Unmarshal(body["interfaces"], &body) != nil {
				w.WriteHeader(400)
				return
			}
		}
		var entries []struct {
			Name   string `json:"name"`
			Config struct {
				Enabled *bool `json:"enabled"`
			} `json:"config"`
			Aggregation struct {
				Config map[string]string `json:"config"`
			} `json:"openconfig-if-aggregate:aggregation"`
		}
		if json.Unmarshal(body["interface"], &entries) != nil || len(entries) != 1 {
			w.WriteHeader(400)
			return
		}
		entry := entries[0]
		if port, ok := s.ports[entry.Name]; ok && entry.Config.Enabled != nil {
			port.enabled = *entry.Config.Enabled
			s.ports[entry.Name] = port
		} else if strings.HasPrefix(entry.Name, "lag ") {
			_, exists := s.names[entry.Name]
			if exists && entry.Aggregation.Config["lag-type"] != "" {
				w.WriteHeader(500)
				return
			}
			if !exists && r.Method != "POST" {
				w.WriteHeader(404)
				return
			}
			s.names[entry.Name] = entry.Aggregation.Config["openconfig-if-aggregate-aug:lag-name"]
			if !exists {
				s.modes[entry.Name] = entry.Aggregation.Config["lag-type"]
			}
		} else {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
		return
	}
	for name, port := range s.ports {
		base := "/interface=" + name + "/ethernet/config"
		if path == base && r.Method == "PATCH" {
			var body struct {
				Config map[string]string `json:"config"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				w.WriteHeader(400)
				return
			}
			target := body.Config["openconfig-if-aggregate:aggregate-id"]
			if s.names[target] == "" || port.aggregate != "" {
				w.WriteHeader(400)
				return
			}
			port.aggregate = target
			s.ports[name] = port
			w.WriteHeader(204)
			return
		}
		if path == base+"/aggregate-id" && r.Method == "DELETE" {
			port.aggregate = ""
			port.enabled = false
			s.ports[name] = port
			w.WriteHeader(204)
			return
		}
	}
	if strings.HasPrefix(path, "/interface=lag ") && r.Method == "DELETE" {
		if s.deleteMissing {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		name := strings.TrimPrefix(path, "/interface=")
		delete(s.names, name)
		delete(s.modes, name)
		for key, port := range s.ports {
			if port.aggregate == name {
				port.aggregate = ""
				port.enabled = false
				s.ports[key] = port
			}
		}
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(404)
}

func TestOpenTofuLAG(t *testing.T) {
	s := newSwitch(t)
	s.lags = &lagSwitch{names: map[string]string{"lag 54": "neighbor"}, modes: map[string]string{"lag 54": "STATIC"}, ports: map[string]lagPort{"ethernet 1/1/7": {enabled: true}, "ethernet 1/1/8": {enabled: false}, "ethernet 1/1/9": {enabled: true, aggregate: "lag 54"}}}
	s.startupLAG = s.lags.configuration()
	write, run, base := tofuFixture(t, s)
	config := func(name, mode string, members ...string) {
		values := []string{}
		for _, member := range members {
			values = append(values, strconv.Quote(member))
		}
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_lag\" \"test\" {\nlag_id=53\nname=%q\nmode=%q\nmembers=[%s]\n}\n", name, mode, strings.Join(values, ",")))
	}
	check := func(present, enabled7 bool, members ...string) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if (s.lags.names["lag 53"] != "") != present {
			t.Errorf("LAG existence: %v", s.lags.names)
		}
		for _, name := range []string{"ethernet 1/1/7", "ethernet 1/1/8"} {
			if (s.lags.ports[name].aggregate == "lag 53") != slices.Contains(members, name) {
				t.Errorf("membership of %s: %+v", name, s.lags.ports[name])
			}
		}
		if s.lags.ports["ethernet 1/1/7"].enabled != enabled7 || s.lags.ports["ethernet 1/1/8"].enabled {
			t.Errorf("unexpected native port state: %+v", s.lags.ports)
		}
		if s.lags.names["lag 54"] != "neighbor" || s.lags.ports["ethernet 1/1/9"].aggregate != "lag 54" {
			t.Error("changed neighboring LAG")
		}
		if s.startupLAG != s.lags.configuration() {
			t.Error("configuration was not persisted")
		}
	}
	config("test", "dynamic", "ethernet 1/1/7", "ethernet 1/1/8")
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(true, true, "ethernet 1/1/7", "ethernet 1/1/8")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_lag.test")
	run(0, "import", "-no-color", "fastiron_lag.test", "lag 53")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	config("renamed", "dynamic", "ethernet 1/1/8")
	s.mu.Lock()
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	// A later process retries persistence without changing detached port state.
	run(0, "apply", "-auto-approve", "-no-color")
	check(true, false, "ethernet 1/1/8")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.lags.names["lag 53"] = "manual"
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	config("renamed", "static", "ethernet 1/1/8")
	run(0, "apply", "-auto-approve", "-no-color")
	check(true, false, "ethernet 1/1/8")
	s.mu.Lock()
	s.lags.child = true
	s.mu.Unlock()
	write("main.tf", base)
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "independent interface configuration") {
		t.Fatalf("missing child guard: %s", out)
	}
	s.mu.Lock()
	exists := s.lags.names["lag 53"] != ""
	s.lags.child = false
	s.failSave = true
	s.mu.Unlock()
	if !exists {
		t.Fatal("guarded deletion removed LAG")
	}
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(false, false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	config("empty", "dynamic")
	run(0, "apply", "-auto-approve", "-no-color")
	check(true, false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	config("empty", "dynamic", "ethernet 1/1/9")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "already belongs to lag 54") {
		t.Fatalf("missing member ownership guard: %s", out)
	}
	check(true, false)

	s.mu.Lock()
	s.lags.ports["ethernet 1/1/7"] = lagPort{enabled: true}
	s.mu.Unlock()
	config("empty", "dynamic", "ethernet 1/1/7")
	run(0, "apply", "-auto-approve", "-no-color")
	check(true, true, "ethernet 1/1/7")

	write("main.tf", base)
	run(0, "apply", "-auto-approve", "-no-color")
	check(false, false)
}

func TestOpenTofuLAGDeleteMissing(t *testing.T) {
	s := newSwitch(t)
	s.lags = &lagSwitch{names: map[string]string{"lag 53": "imported"}, modes: map[string]string{"lag 53": "STATIC"}, ports: map[string]lagPort{"ethernet 1/1/7": {enabled: true, aggregate: "lag 53"}}, deleteMissing: true}
	original := s.lags.configuration()
	write, run, base := tofuFixture(t, s)
	base = strings.Replace(base, `provider "fastiron" {`, `provider "fastiron" {
 operation_timeout = "2s"`, 1)
	write("main.tf", base+`resource "fastiron_lag" "test" {
 lag_id = 53
 name = "imported"
 mode = "static"
 members = ["ethernet 1/1/7"]
}
`)
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_lag.test", "lag 53")
	write("main.tf", base)
	for range 2 {
		out := run(1, "apply", "-auto-approve", "-no-color")
		if !strings.Contains(out, "RESTCONF cannot delete native lag 53") {
			t.Fatalf("missing native deletion diagnostic: %s", out)
		}
		s.mu.Lock()
		unchanged := s.lags.configuration() == original && s.startupLAG == ""
		s.mu.Unlock()
		if !unchanged {
			t.Fatal("refused deletion changed running or saved configuration")
		}
	}
	if out := run(0, "state", "show", "fastiron_lag.test"); !strings.Contains(out, "persistence_pending = true") {
		t.Fatalf("failed deletion did not retain pending state: %s", out)
	}
	// External CLI removal lets a later apply finish the pending deletion.
	s.mu.Lock()
	delete(s.lags.names, "lag 53")
	delete(s.lags.modes, "lag 53")
	s.lags.ports["ethernet 1/1/7"] = lagPort{enabled: false}
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if out := run(0, "state", "list"); strings.Contains(out, "fastiron_lag.test") {
		t.Fatalf("retained deleted LAG: %s", out)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startupLAG != s.lags.configuration() || s.lags.ports["ethernet 1/1/7"].enabled {
		t.Fatal("cleanup did not persist native disabled-member state")
	}
}
