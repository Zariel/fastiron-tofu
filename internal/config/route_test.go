package config_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestIPv4Routes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		distance int64
		options  bool
	}{
		{"default", 1, false},
		{"distance", 200, false},
		{"distance-drift", 202, false},
		{"mask", 200, false},
		{"metric", 200, true},
		{"name", 1, true},
		{"tag", 1, true},
		{"distance255", 255, false},
		{"combined", 200, true},
		{"quoted-name", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "routes", tc.name+".conf"))
			if err != nil {
				t.Fatal(err)
			}
			routes, err := configtest.Parse(t, string(raw)).IPv4Routes()
			if err != nil {
				t.Fatal(err)
			}
			if len(routes) != 2 {
				t.Fatalf("routes=%v", routes)
			}
			expected := map[netip.Addr]config.IPv4Route{
				netip.MustParseAddr("192.0.2.2"): {Prefix: netip.MustParsePrefix("198.18.53.0/24"), NextHop: netip.MustParseAddr("192.0.2.2"), Distance: tc.distance, HasOptions: tc.options},
				netip.MustParseAddr("192.0.2.3"): {Prefix: netip.MustParsePrefix("198.18.53.0/24"), NextHop: netip.MustParseAddr("192.0.2.3"), Distance: 201},
			}
			for _, route := range routes {
				if route != expected[route.NextHop] {
					t.Errorf("route=%+v; want %+v", route, expected[route.NextHop])
				}
			}
		})
	}
}

func TestIPv4RouteSyntax(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		distance   int64
		options    bool
	}{
		{"mask", "ip route 198.18.53.0 255.255.255.0 192.0.2.2 distance 200", 200, false},
		{"default route", "ip route 0.0.0.0/0 192.0.2.2", 1, false},
		{"unusable", "ip route 198.18.53.0/24 192.0.2.2 distance 255", 255, false},
		{"bfd", "ip route 198.18.53.0/24 192.0.2.2 bfd", 1, true},
		{"combined", "ip route 198.18.53.0/24 192.0.2.2 7 bfd distance 200 name ROUTE tag 4294967295", 200, true},
		{"quoted keyword", `ip route 198.18.53.0/24 192.0.2.2 name "tag 4294967296"`, 1, true},
		{"tag before name", `ip route 198.18.53.0/24 192.0.2.2 tag 53 name "TOFU ROUTE"`, 1, true},
		{"tag after name", `ip route 198.18.53.0/24 192.0.2.2 name "TOFU ROUTE" tag 53`, 1, true},
		{"next-hop VRF", "ip route 198.18.53.0/24 next-hop-vrf blue 192.0.2.2", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes, err := configtest.Parse(t, "ver 09.0.10k\n"+tc.line+"\nend").IPv4Routes()
			if err != nil || len(routes) != 1 {
				t.Fatalf("routes=%v error=%v", routes, err)
			}
			if routes[0].Distance != tc.distance || routes[0].HasOptions != tc.options {
				t.Fatalf("route=%+v", routes[0])
			}
		})
	}
}

func TestIPv4RouteMalformed(t *testing.T) {
	for _, line := range []string{
		"ip route", "ip route 198.18.53.0/24", "ip route 198.18.53.0/24 192.0.2.2 distance",
		"ip route 198.18.53.1/24 192.0.2.2", "ip route 198.18.53.0 255.0.255.0 192.0.2.2",
		"ip route 198.18.53.0/24 192.0.2.999", "ip route 198.18.53.0/24 0.0.0.0",
		"ip route 198.18.53.0/24 192.0.2.2 distance 256", "ip route 198.18.53.0/24 192.0.2.2 distance 0",
		"ip route 198.18.53.0/24 192.0.2.2 17", "ip route 198.18.53.0/24 192.0.2.2 tag 4294967296",
		"ip route 198.18.53.0/24 192.0.2.2 distance 200 distance 201", "ip route 198.18.53.0/24 192.0.2.2 unknown",
		`ip route 198.18.53.0/24 192.0.2.2 name "unterminated`,
		`ip route 198.18.53.0/24 192.0.2.2 name "TOFU" extra`,
		`ip route 198.18.53.0/24 192.0.2.2 tag 53 name ROUTE tag 54`,
	} {
		t.Run(line, func(t *testing.T) {
			if _, err := configtest.Parse(t, "ver 09.0.10k\n"+line+"\nend").IPv4Routes(); err == nil {
				t.Fatal("accepted malformed route")
			}
		})
	}
}

func TestIPv4RouteScope(t *testing.T) {
	document := configtest.Parse(t, `ver 09.0.10k
ip route vrf blue 198.18.53.0/24 192.0.2.2
ip route 198.18.54.0/24 null0
ip route 198.18.55.0/24 ve 5 7 distance 200
ip route 198.18.53.0/24 192.0.2.2
end`)
	routes, err := document.IPv4Routes()
	if err != nil || len(routes) != 1 || routes[0].Prefix != netip.MustParsePrefix("198.18.53.0/24") {
		t.Fatalf("routes=%v error=%v", routes, err)
	}
	duplicate := configtest.Parse(t, "ver 09.0.10k\nip route 198.18.53.0/24 192.0.2.2\nip route 198.18.53.0/24 192.0.2.2 distance 200\nend")
	if _, err := duplicate.IPv4Routes(); err == nil {
		t.Fatal("accepted duplicate route identity")
	}
}
