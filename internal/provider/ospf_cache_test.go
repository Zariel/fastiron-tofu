package provider

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestOpenTofuOSPFCache(t *testing.T) {
	s := newSwitch(t)
	s.ospf = &ospfSwitch{
		areas:   map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {"ve 53"}},
		startup: map[string][]string{},
		cached:  map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.54": {"ve 54"}},
	}
	write, run, base := tofuFixture(t, s)
	const query = `data "fastiron_ospf_areas" "test" { depends_on = [fastiron_router_ospf_interface.test] }
output "areas" { value = data.fastiron_ospf_areas.test.areas }
`
	const resource = `resource "fastiron_router_ospf_area" "test" { area_id = "0.0.0.53" }
resource "fastiron_router_ospf_interface" "test" {
 area_id = fastiron_router_ospf_area.test.area_id
 interface = "ve 53"
}
`
	write("main.tf", base+query+resource)
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_router_ospf_area.test", "0.0.0.53")
	run(0, "import", "-no-color", "fastiron_router_ospf_interface.test", "0.0.0.53|ve 53")
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "areas")); got != `{"0.0.0.0":{"interfaces":["ve 5"]},"0.0.0.53":{"interfaces":["ve 53"]}}` {
		t.Fatalf("query followed cache: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.ospf.missingProtocol = true
	s.mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.ospf.missingProtocol = false
	if len(s.ospf.startup) != 0 {
		t.Fatal("read or import saved configuration")
	}
	// A cached binding must not hide an independent native removal.
	s.ospf.cached = map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {"ve 53"}}
	s.ospf.areas["0.0.0.53"] = []string{}
	s.ospf.ignoreWrites = true
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "OSPF interface binding did not converge") {
		t.Fatalf("missing native convergence diagnostic: %s", out)
	}
	s.mu.Lock()
	if len(s.ospf.startup) != 0 {
		t.Fatal("unconfirmed write saved configuration")
	}
	s.ospf.ignoreWrites = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	if !maps.EqualFunc(s.ospf.areas, s.ospf.startup, slices.Equal[[]string]) {
		t.Fatal("binding retry was not persisted")
	}
	delete(s.ospf.areas, "0.0.0.53")
	s.ospf.ignoreWrites = true
	s.mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "OSPF area did not converge") {
		t.Fatalf("missing area convergence diagnostic: %s", out)
	}
	s.mu.Lock()
	if _, ok := s.ospf.startup["0.0.0.53"]; !ok {
		t.Fatal("unconfirmed area write saved native removal")
	}
	s.ospf.ignoreWrites = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
}
