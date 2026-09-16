package provider

import (
	"fmt"
	"strings"
	"testing"
)

func TestOpenTofuOSPFAreaQuery(t *testing.T) {
	s := newSwitch(t)
	s.ospf = &ospfSwitch{
		areas:   map[string][]string{"0.0.0.53": {"ve 53"}},
		cached:  map[string][]string{},
		startup: map[string][]string{},
	}
	write, run, base := tofuFixture(t, s)
	choose := func(id string) {
		write("main.tf", base+fmt.Sprintf(`data "fastiron_ospf_area" "test" { area_id = %q }
output "area" { value = data.fastiron_ospf_area.test }
`, id))
	}
	choose("0.0.0.53")
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "area")); got != `{"area_id":"0.0.0.53","id":"0.0.0.53","interfaces":["ve 53"]}` {
		t.Fatalf("query ignored native binding: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	s.mu.Lock()
	s.ospf.areas["0.0.0.53"] = []string{}
	s.ospf.cached["0.0.0.53"] = []string{"ve 53"}
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "area")); got != `{"area_id":"0.0.0.53","id":"0.0.0.53","interfaces":[]}` {
		t.Fatalf("query retained cached binding: %s", got)
	}

	s.mu.Lock()
	delete(s.ospf.areas, "0.0.0.53")
	s.mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "OSPF area not found") {
		t.Fatalf("cached ghost accepted: %s", out)
	}
	for _, id := range []string{"53", "0.0.0.053", "::1", "0.0.0.256"} {
		choose(id)
		if out := run(1, "plan", "-no-color"); !strings.Contains(out, "Invalid OSPF area") {
			t.Fatalf("missing identity diagnostic: %s", out)
		}
	}
	s.mu.Lock()
	mutations, saved := s.ospf.mutations, len(s.ospf.startup)
	s.mu.Unlock()
	if mutations != 0 || saved != 0 {
		t.Fatalf("query mutated or saved configuration: writes=%d saved=%d", mutations, saved)
	}
}
