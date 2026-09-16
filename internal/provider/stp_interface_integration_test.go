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
	stpPortOptions struct{ edge, bpdu, root bool }
	stpPortSwitch  struct {
		running, startup       map[string]stpPortOptions
		failPatch, ignorePatch bool
		ignoreDelete           bool
	}
)

func stpPortConfiguration(base string, ports map[string]stpPortOptions) string {
	for _, name := range slices.Sorted(maps.Keys(ports)) {
		p := ports[name]
		lines := ""
		if p.edge {
			lines += " spanning-tree 802-1w admin-edge-port\n"
		}
		if p.bpdu {
			lines += " stp-bpdu-guard\n"
		}
		if p.root {
			lines += " spanning-tree root-protect\n"
		}
		header := "interface " + name + "\n"
		if strings.Contains(base, header) {
			base = strings.Replace(base, header, header+lines, 1)
		} else {
			base = strings.TrimSuffix(base, "end") + header + lines + " port-name NEIGHBOR\n!\nend"
		}
	}
	return base
}

func (s *stpPortSwitch) rest(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		name := strings.TrimPrefix(r.URL.Path, "/restconf/data/stp/interfaces/interface=")
		if !s.ignoreDelete {
			delete(s.running, name)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == "GET" {
		entries := []any{}
		for name, p := range s.running {
			edge, guard := "openconfig-spanning-tree-types:EDGE_DISABLE", "NONE"
			if p.edge {
				edge = "openconfig-spanning-tree-types:EDGE_ENABLE"
			}
			if p.root {
				guard = "ROOT"
			}
			entries = append(entries, map[string]any{"name": name, "config": map[string]any{"name": name, "edge-port": edge, "guard": guard, "bpdu-guard": p.bpdu}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-spanning-tree:interfaces": map[string]any{"interface": entries}})
		return
	}
	if r.Method != "PATCH" {
		http.Error(w, "unsupported method", 405)
		return
	}
	if s.failPatch {
		http.Error(w, "write failed", 500)
		return
	}
	var body struct {
		Interfaces struct {
			Interface []struct {
				Name   string `json:"name"`
				Config struct {
					Name  string `json:"name"`
					Edge  string `json:"edge-port"`
					Guard string `json:"guard"`
					BPDU  *bool  `json:"bpdu-guard"`
				} `json:"config"`
			} `json:"interface"`
		} `json:"interfaces"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Interfaces.Interface) != 1 {
		http.Error(w, "invalid patch", 400)
		return
	}
	entry := body.Interfaces.Interface[0]
	c := entry.Config
	if c.Name != entry.Name || c.BPDU == nil || (c.Edge != "openconfig-spanning-tree-types:EDGE_ENABLE" && c.Edge != "openconfig-spanning-tree-types:EDGE_DISABLE") || (c.Guard != "ROOT" && c.Guard != "NONE") {
		http.Error(w, "invalid options", 400)
		return
	}
	if !s.ignorePatch {
		s.running[entry.Name] = stpPortOptions{edge: c.Edge == "openconfig-spanning-tree-types:EDGE_ENABLE", root: c.Guard == "ROOT", bpdu: *c.BPDU}
	}
	w.WriteHeader(204)
}

func TestOpenTofuSTPInterface(t *testing.T) {
	s := newSwitch(t)
	neighbor := stpPortOptions{root: true}
	s.stpPorts = &stpPortSwitch{running: map[string]stpPortOptions{"ethernet 1/1/3": neighbor}, startup: map[string]stpPortOptions{"ethernet 1/1/3": neighbor}}
	write, run, base := tofuFixture(t, s)
	config := func(name, fields string) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_spanning_tree_interface" "test" {
 interface = %q
 %s
}
`, name, fields))
	}
	check := func(name string, want stpPortOptions) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stpPorts.running[name] != want || s.stpPorts.startup[name] != want {
			t.Fatalf("port %s running=%v startup=%v want=%v", name, s.stpPorts.running[name], s.stpPorts.startup[name], want)
		}
		if s.ethernet["description"] != "manual port" || s.ethernet["enabled"] != true {
			t.Fatal("STP changed base interface configuration")
		}
	}

	config("ethernet 1/1/2", "admin_edge = true\nbpdu_guard = true\nroot_guard = true")
	run(0, "init", "-no-color")
	config("ethernet 1/1/99", "")
	run(1, "plan", "-no-color")
	config("ethernet 1/1/2", "admin_edge = true\nbpdu_guard = true\nroot_guard = true")
	run(0, "apply", "-auto-approve", "-no-color")
	check("ethernet 1/1/2", stpPortOptions{true, true, true})
	check("ethernet 1/1/3", neighbor)
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_spanning_tree_interface.test")
	run(0, "import", "-no-color", "fastiron_spanning_tree_interface.test", "ethernet 1/1/2")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	config("ethernet 1/1/2", "bpdu_guard = true")
	run(0, "apply", "-auto-approve", "-no-color")
	check("ethernet 1/1/2", stpPortOptions{bpdu: true})
	check("ethernet 1/1/3", neighbor)

	s.mu.Lock()
	s.stpPorts.running["ethernet 1/1/2"] = stpPortOptions{}
	s.stpPorts.startup["ethernet 1/1/2"] = stpPortOptions{}
	s.stpPorts.ignorePatch = true
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(1, "apply", "-auto-approve", "-no-color")

	s.mu.Lock()
	s.stpPorts.ignorePatch = false
	s.falseSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")

	s.mu.Lock()
	s.falseSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check("ethernet 1/1/2", stpPortOptions{bpdu: true})

	config("ethernet 1/1/3", "admin_edge = true")
	run(0, "apply", "-auto-approve", "-no-color")
	check("ethernet 1/1/2", stpPortOptions{})
	check("ethernet 1/1/3", stpPortOptions{edge: true})

	write("main.tf", base)
	s.mu.Lock()
	s.stpPorts.failPatch = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	check("ethernet 1/1/3", stpPortOptions{edge: true})
	s.mu.Lock()
	s.stpPorts.failPatch = false
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")

	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check("ethernet 1/1/3", stpPortOptions{})
	run(0, "plan", "-detailed-exitcode", "-no-color")
}

func TestOpenTofuSTPDefaultEntry(t *testing.T) {
	s := newSwitch(t)
	name := "ethernet 1/1/2"
	s.stpPorts = &stpPortSwitch{running: map[string]stpPortOptions{name: {}}, startup: map[string]stpPortOptions{name: {}}, ignoreDelete: true}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_spanning_tree_interface" "test" { interface = "ethernet 1/1/2" }`)
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_spanning_tree_interface.test", name)
	write("main.tf", base)

	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "retained the spanning-tree interface entry") {
		t.Fatalf("missing retained-entry diagnostic: %s", out)
	}
	if out := run(0, "state", "show", "-no-color", "fastiron_spanning_tree_interface.test"); !strings.Contains(out, "persistence_pending = true") {
		t.Fatalf("failed entry deletion did not retain state: %s", out)
	}
	s.mu.Lock()
	s.stpPorts.ignoreDelete = false
	s.mu.Unlock()

	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.stpPorts.running[name]; exists {
		t.Fatal("default STP entry still references interface")
	}
	if _, exists := s.stpPorts.startup[name]; exists {
		t.Fatal("entry deletion was not persisted")
	}
}
