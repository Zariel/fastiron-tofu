package provider

import (
	"fmt"
	"maps"
	"strings"
	"testing"
)

func TestOpenTofuRouteQuery(t *testing.T) {
	const identity = "198.18.53.0/24|192.0.2.2"
	s := newSwitch(t)
	s.routes = &routeSwitch{
		protocol: true,
		running:  map[string]int64{identity: 200, "198.18.53.0/24|192.0.2.3": 201},
		cached:   map[string]int64{identity: 1},
		startup:  map[string]int64{},
		extra:    ` name "QUERY ROUTE"`,
	}
	write, run, base := tofuFixture(t, s)
	choose := func(prefix, hop string) {
		write("main.tf", base+fmt.Sprintf(`data "fastiron_static_route" "test" {
 prefix = %q
 next_hop = %q
}
output "route" { value = data.fastiron_static_route.test }
`, prefix, hop))
	}
	unchanged := func() {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.routes.startup) != 0 {
			t.Fatal("query saved native configuration")
		}
		if s.routes.mutations != 0 {
			t.Fatal("query issued a configuration mutation")
		}
	}
	choose("198.18.53.0/24", "192.0.2.2")
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "route")); got != `{"distance":200,"id":"198.18.53.0/24|192.0.2.2","next_hop":"192.0.2.2","prefix":"198.18.53.0/24"}` {
		t.Fatalf("query used cached distance: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	unchanged()

	s.mu.Lock()
	delete(s.routes.running, identity)
	s.mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "Static route not found") {
		t.Fatalf("cached ghost was accepted: %s", out)
	}
	unchanged()

	s.mu.Lock()
	s.routes.running[identity] = 202
	s.routes.cached = map[string]int64{}
	before := maps.Clone(s.routes.running)
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "route")); got != `{"distance":202,"id":"198.18.53.0/24|192.0.2.2","next_hop":"192.0.2.2","prefix":"198.18.53.0/24"}` {
		t.Fatalf("native-only route missing: %s", got)
	}
	unchanged()

	for _, tc := range []struct{ prefix, hop string }{
		{"198.18.53.1/24", "192.0.2.2"},
		{"2001:db8::/64", "192.0.2.2"},
		{"198.18.53.0/24", "0.0.0.0"},
		{"198.18.53.0/24", "224.0.0.1"},
	} {
		choose(tc.prefix, tc.hop)
		if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "Invalid route identity") {
			t.Fatalf("invalid selector accepted: %s", out)
		}
	}
	unchanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !maps.Equal(s.routes.running, before) {
		t.Fatal("query changed native routes")
	}
}
