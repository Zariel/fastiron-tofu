package route

import (
	"net/netip"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestRouteRemoval(t *testing.T) {
	desired := route{Prefix: netip.MustParsePrefix("198.18.53.0/24"), NextHop: netip.MustParseAddr("192.0.2.2"), Distance: 200}
	for _, tc := range []struct {
		name, native string
		allowed      bool
	}{
		{"owned", "ip route 198.18.53.0/24 192.0.2.2 distance 200", true},
		{"dotted mask", "ip route 198.18.53.0 255.255.255.0 192.0.2.2 distance 200", true},
		{"distance drift", "ip route 198.18.53.0/24 192.0.2.2", false},
		{"additional cost", "ip route 198.18.53.0/24 192.0.2.2 7 distance 200", false},
		{"additional tag", "ip route 198.18.53.0/24 192.0.2.2 distance 200 tag 53", false},
		{"missing", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := configtest.Parse(t, "ver 09.0.10k\n"+tc.native+"\nip route 198.18.53.0/24 192.0.2.3 distance 201 name NEIGHBOR\nend")
			if err := routeOptions(document, desired); (err == nil) != tc.allowed {
				t.Fatalf("removal error=%v; want allowed=%t", err, tc.allowed)
			}
		})
	}
}
