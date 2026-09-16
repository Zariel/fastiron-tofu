package provider

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestOpenTofuOSPFAreaPreservation(t *testing.T) {
	s := newSwitch(t)
	s.ospf = &ospfSwitch{
		areas:       map[string][]string{"0.0.0.0": {"ve 5"}},
		startup:     map[string][]string{"0.0.0.0": {"ve 5"}},
		corruptArea: true,
	}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_router_ospf_area" "test" { area_id = "0.0.0.53" }`)
	run(0, "init", "-no-color")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "OSPF area operation changed unrelated native configuration") {
		t.Fatalf("missing preservation error: %s", out)
	}
	if out := run(0, "state", "show", "fastiron_router_ospf_area.test"); !strings.Contains(out, "persistence_pending = true") {
		t.Fatalf("partially created area is not addressable for retry: %s", out)
	}
	s.mu.Lock()
	_, running := s.ospf.areas["0.0.0.53"]
	_, saved := s.ospf.startup["0.0.0.53"]
	s.mu.Unlock()
	if !running || saved {
		t.Fatalf("area presence: running=%v saved=%v", running, saved)
	}

	// Repair the independently owned range before retrying persistence.
	s.mu.Lock()
	s.ospf.corruptArea, s.ospf.areaOptions = false, false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	persisted := maps.EqualFunc(s.ospf.areas, s.ospf.startup, slices.Equal[[]string])
	s.mu.Unlock()
	if !persisted {
		t.Fatal("repaired area was not persisted")
	}
}

func TestOpenTofuOSPFBindingPreservation(t *testing.T) {
	s := newSwitch(t)
	s.ospf = &ospfSwitch{
		areas:          map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {}},
		startup:        map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {}},
		corruptBinding: true,
	}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_router_ospf_interface" "test" {
 area_id = "0.0.0.53"
 interface = "ve 53"
}
`)
	run(0, "init", "-no-color")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "OSPF binding operation changed unrelated native configuration") {
		t.Fatalf("missing preservation error: %s", out)
	}
	if out := run(0, "state", "show", "fastiron_router_ospf_interface.test"); !strings.Contains(out, "persistence_pending = true") {
		t.Fatalf("partially created binding is not addressable for retry: %s", out)
	}
	s.mu.Lock()
	running := slices.Clone(s.ospf.areas["0.0.0.53"])
	saved := slices.Clone(s.ospf.startup["0.0.0.53"])
	s.mu.Unlock()
	if !slices.Equal(running, []string{"ve 53"}) || len(saved) != 0 {
		t.Fatalf("running=%v saved=%v", running, saved)
	}

	// Repair the independently owned option before retrying persistence.
	s.mu.Lock()
	s.ospf.corruptBinding, s.ospf.interfaceOptions = false, false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	persisted := maps.EqualFunc(s.ospf.areas, s.ospf.startup, slices.Equal[[]string])
	s.mu.Unlock()
	if !persisted {
		t.Fatal("repaired binding was not persisted")
	}
}
