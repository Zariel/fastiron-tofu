package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

type routeSwitch struct {
	protocol         bool
	running, startup map[string]int64
	cached           map[string]int64
	ignoreWrites     bool
	mutations        int
	extra            string
	extraTarget      string
	corruptNeighbor  bool
}

func routeConfiguration(routes map[string]int64, extra, extraTarget string) string {
	keys := slices.Sorted(maps.Keys(routes))
	var b strings.Builder
	for _, key := range keys {
		prefix, gateway, _ := strings.Cut(key, "|")
		options := extra
		if extraTarget != "" && extraTarget != key {
			options = ""
		}
		fmt.Fprintf(&b, "ip route %s %s distance %d%s\n", prefix, gateway, routes[key], options)
	}
	return b.String()
}

func (s *routeSwitch) rest(w http.ResponseWriter, r *http.Request) {
	const protocols = "/restconf/data/network-instances/network-instance=default-vrf/protocols"
	const collection = protocols + "/protocol=STATIC,icx-static/static-routes"
	endpoint := r.URL.EscapedPath()
	if r.Method == "GET" {
		if endpoint == protocols {
			entries := []any{}
			if s.protocol {
				entries = append(entries, map[string]any{"identifier": "openconfig-policy-types:STATIC", "name": "icx-static"})
			}
			json.NewEncoder(w).Encode(map[string]any{"openconfig-network-instance:protocols": map[string]any{"protocol": entries}})
			return
		}
		if endpoint != collection || !s.protocol {
			http.NotFound(w, r)
			return
		}
		prefixes := map[string][]any{}
		routes := s.running
		if s.cached != nil {
			routes = s.cached
		}
		for key, distance := range routes {
			prefix, gateway, _ := strings.Cut(key, "|")
			prefixes[prefix] = append(prefixes[prefix], map[string]any{"index": gateway, "config": map[string]any{"index": gateway, "next-hop": gateway, "metric": distance}})
		}
		entries := []any{}
		for prefix, hops := range prefixes {
			entries = append(entries, map[string]any{"prefix": prefix, "config": map[string]any{"prefix": prefix}, "next-hops": map[string]any{"next-hop": hops}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-network-instance:static-routes": map[string]any{"static": entries}})
		return
	}
	s.mutations++
	if s.corruptNeighbor {
		s.extra = " name CHANGED"
	}
	if s.ignoreWrites {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == "DELETE" {
		key, ok := strings.CutPrefix(endpoint, collection+"/static=")
		prefix, gateway, split := strings.Cut(key, "/next-hops/next-hop=")
		if !ok || !split {
			http.Error(w, "only keyed next-hop deletion is supported", 400)
			return
		}
		prefix, _ = url.PathUnescape(prefix)
		gateway, _ = url.PathUnescape(gateway)
		delete(s.running, prefix+"|"+gateway)
		delete(s.cached, prefix+"|"+gateway)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var body map[string]json.RawMessage
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "invalid payload", 400)
		return
	}
	var data json.RawMessage
	switch {
	case endpoint == protocols && r.Method == "POST":
		if s.protocol {
			http.Error(w, "protocol exists", 409)
			return
		}
		var protocol struct {
			Routes json.RawMessage `json:"static-routes"`
		}
		if json.Unmarshal(body["protocol"], &protocol) != nil {
			http.Error(w, "invalid protocol", 400)
			return
		}
		data = protocol.Routes
		s.protocol = true
	case endpoint == collection && r.Method == "POST":
		data, _ = json.Marshal(body)
	case endpoint == collection && r.Method == "PATCH":
		data = body["openconfig-network-instance:static-routes"]
	default:
		http.Error(w, "unsupported route operation", 400)
		return
	}
	var routes struct {
		Static []struct {
			Prefix   string `json:"prefix"`
			NextHops struct {
				NextHop []struct {
					Index  string `json:"index"`
					Config struct {
						Metric int64 `json:"metric"`
					} `json:"config"`
				} `json:"next-hop"`
			} `json:"next-hops"`
		} `json:"static"`
	}
	if json.Unmarshal(data, &routes) != nil || len(routes.Static) == 0 {
		http.Error(w, "missing routes", 400)
		return
	}
	for _, route := range routes.Static {
		if r.Method == "POST" && endpoint == collection {
			for key := range s.running {
				if strings.HasPrefix(key, route.Prefix+"|") {
					http.Error(w, "prefix exists", 409)
					return
				}
			}
		}
		for _, hop := range route.NextHops.NextHop {
			key := route.Prefix + "|" + hop.Index
			if _, exists := s.running[key]; exists {
				http.Error(w, "updating a next hop is unsupported", 500)
				return
			}
			s.running[key] = hop.Config.Metric
			if s.cached != nil {
				s.cached[key] = hop.Config.Metric
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestOpenTofuRoutes(t *testing.T) {
	s := newSwitch(t)
	s.routes = &routeSwitch{running: map[string]int64{}, startup: map[string]int64{}}
	write, run, base := tofuFixture(t, s)
	config := func(distance int) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_ip_route" "test" {
 prefix = "198.18.53.0/24"
 next_hop = "192.0.2.2"
 distance = %d
}
data "fastiron_static_routes" "test" {
 depends_on = [fastiron_ip_route.test]
}
output "routes" { value = data.fastiron_static_routes.test.routes }
`, distance))
	}
	check := func(want map[string]int64) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !maps.Equal(s.routes.running, want) || !maps.Equal(s.routes.startup, want) {
			t.Fatalf("routes running=%v startup=%v want=%v", s.routes.running, s.routes.startup, want)
		}
	}
	config(200)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int64{"198.18.53.0/24|192.0.2.2": 200})
	if output := run(0, "output", "-json", "routes"); !strings.Contains(output, `"distance":200`) || !strings.Contains(output, `"next_hop":"192.0.2.2"`) {
		t.Fatalf("route output=%s", output)
	}
	run(0, "state", "rm", "fastiron_ip_route.test")
	run(0, "import", "-no-color", "fastiron_ip_route.test", "198.18.53.0/24|192.0.2.2")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	s.mu.Lock()
	s.routes.running["198.18.53.0/24|192.0.2.3"] = 201
	s.routes.startup["198.18.53.0/24|192.0.2.3"] = 201
	s.mu.Unlock()
	config(202)
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int64{"198.18.53.0/24|192.0.2.2": 202, "198.18.53.0/24|192.0.2.3": 201})
	s.mu.Lock()
	delete(s.routes.running, "198.18.53.0/24|192.0.2.2")
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int64{"198.18.53.0/24|192.0.2.2": 202, "198.18.53.0/24|192.0.2.3": 201})

	write("main.tf", base)
	s.mu.Lock()
	s.routes.extra = " tag 42"
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	check(map[string]int64{"198.18.53.0/24|192.0.2.2": 202, "198.18.53.0/24|192.0.2.3": 201})
	s.mu.Lock()
	s.routes.extra = ""
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int64{"198.18.53.0/24|192.0.2.3": 201})
	run(0, "plan", "-detailed-exitcode", "-no-color")
}

func TestOpenTofuRouteCache(t *testing.T) {
	const target = "198.18.53.0/24|192.0.2.2"
	const neighbor = "198.18.53.0/24|192.0.2.3"
	s := newSwitch(t)
	s.routes = &routeSwitch{
		protocol: true,
		running:  map[string]int64{target: 200, neighbor: 201},
		startup:  map[string]int64{target: 200, neighbor: 201},
		cached:   map[string]int64{target: 1, neighbor: 201},
	}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_ip_route" "test" {
 prefix = "198.18.53.0/24"
 next_hop = "192.0.2.2"
 distance = 200
}
data "fastiron_static_routes" "test" { depends_on = [fastiron_ip_route.test] }
output "routes" { value = data.fastiron_static_routes.test.routes }
`)
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_ip_route.test", target)
	if out := run(0, "state", "show", "fastiron_ip_route.test"); !strings.Contains(out, "distance            = 200") {
		t.Fatalf("import did not report native distance: %s", out)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	// Native presence remains authoritative while the RESTCONF entry is missing.
	s.mu.Lock()
	delete(s.routes.cached, target)
	s.mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.routes.protocol = false
	s.mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.routes.protocol = true
	s.routes.cached[target] = 1
	s.routes.running[target] = 202
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	restored := maps.Equal(s.routes.running, map[string]int64{target: 200, neighbor: 201}) && maps.Equal(s.routes.running, s.routes.startup)
	s.mu.Unlock()
	if !restored {
		t.Fatal("distance replacement did not persist native state and preserve neighbor")
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	// A cached desired route must not hide native deletion or an ignored repair.
	s.mu.Lock()
	delete(s.routes.running, target)
	s.routes.ignoreWrites = true
	saved := maps.Clone(s.routes.startup)
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "static route did not converge") {
		t.Fatalf("missing native convergence failure: %s", out)
	}
	s.mu.Lock()
	unchanged := maps.Equal(s.routes.running, map[string]int64{neighbor: 201}) && maps.Equal(s.routes.startup, saved)
	s.routes.ignoreWrites = false
	s.mu.Unlock()
	if !unchanged {
		t.Fatal("ignored write changed or persisted native configuration")
	}
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !maps.Equal(s.routes.running, map[string]int64{target: 200, neighbor: 201}) || !maps.Equal(s.routes.running, s.routes.startup) {
		t.Fatal("retry did not persist repaired route and preserve neighbor")
	}
}

func TestOpenTofuRoutePreservation(t *testing.T) {
	const target = "198.18.53.0/24|192.0.2.2"
	const neighbor = "198.18.53.0/24|192.0.2.3"
	s := newSwitch(t)
	s.routes = &routeSwitch{protocol: true, running: map[string]int64{neighbor: 201}, startup: map[string]int64{neighbor: 201}, extra: " name KEEP", extraTarget: neighbor, corruptNeighbor: true}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_ip_route" "test" {
 prefix = "198.18.53.0/24"
 next_hop = "192.0.2.2"
 distance = 200
}
`)
	run(0, "init", "-no-color")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "changed unrelated native configuration") {
		t.Fatalf("missing preservation diagnostic: %s", out)
	}
	if out := run(0, "state", "show", "fastiron_ip_route.test"); !strings.Contains(out, "persistence_pending = true") {
		t.Fatalf("partial creation lost pending state: %s", out)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !maps.Equal(s.routes.running, map[string]int64{target: 200, neighbor: 201}) || s.routes.extra != " name CHANGED" {
		t.Fatal("fixture did not expose the applied route and changed neighbor option")
	}
	if !maps.Equal(s.routes.startup, map[string]int64{neighbor: 201}) {
		t.Fatal("failed preservation check saved the partial write")
	}
}
