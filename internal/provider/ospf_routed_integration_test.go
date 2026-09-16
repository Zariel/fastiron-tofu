package provider

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestOpenTofuOSPFRoutedBindings(t *testing.T) {
	s := newSwitch(t)
	s.ospf = &ospfSwitch{
		areas:   map[string][]string{"0.0.0.0": {"ve 5"}},
		startup: map[string][]string{"0.0.0.0": {"ve 5"}},
	}
	write, run, base := tofuFixture(t, s)
	configuration := `resource "fastiron_router_ospf_area" "test" { area_id = "0.0.0.59" }
resource "fastiron_router_ospf_interface" "test" {
 for_each = toset(["ethernet 1/1/9", "lag 59", "loopback 32"])
 area_id = fastiron_router_ospf_area.test.area_id
 interface = each.value
}
data "fastiron_ospf_area" "test" {
 area_id = fastiron_router_ospf_area.test.area_id
 depends_on = [fastiron_router_ospf_interface.test]
}
output "area" { value = data.fastiron_ospf_area.test }
`
	check := func(want map[string][]string) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		equal := func(a, b []string) bool {
			return slices.Equal(slices.Sorted(slices.Values(a)), b)
		}
		if !maps.EqualFunc(s.ospf.areas, want, equal) || !maps.EqualFunc(s.ospf.startup, want, equal) {
			t.Fatalf("running=%v startup=%v want=%v", s.ospf.areas, s.ospf.startup, want)
		}
	}

	write("main.tf", base+configuration)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.59": {"ethernet 1/1/9", "lag 59", "loopback 32"}})
	if output := strings.TrimSpace(run(0, "output", "-json", "area")); output != `{"area_id":"0.0.0.59","id":"0.0.0.59","interfaces":["ethernet 1/1/9","lag 59","loopback 32"]}` {
		t.Fatalf("area=%s", output)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	write("main.tf", base+strings.Replace(configuration, `toset(["ethernet 1/1/9", "lag 59", "loopback 32"])`, `toset(["lag 59", "loopback 32"])`, 1))
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.59": {"lag 59", "loopback 32"}})
	run(0, "plan", "-detailed-exitcode", "-no-color")

	write("main.tf", base)
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}})
}
