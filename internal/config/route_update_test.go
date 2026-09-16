package config_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestCapturedRouteUpdate(t *testing.T) {
	before, err := os.ReadFile(filepath.Join("testdata", "routes", "workflow-distance-drift.conf"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join("testdata", "routes", "workflow-drift-repaired.conf"))
	if err != nil {
		t.Fatal(err)
	}
	err = configtest.Parse(t, string(after)).CheckIPv4RouteUpdate(configtest.Parse(t, string(before)), netip.MustParsePrefix("198.18.54.0/24"), netip.MustParseAddr("192.0.2.2"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestIPv4RouteUpdate(t *testing.T) {
	const before = `ver 09.0.10k
ip route 198.18.53.0/24 192.0.2.2 distance 200
ip route 198.18.53.0/24 192.0.2.3 distance 201 name KEEP
ip route vrf blue 198.18.54.0/24 192.0.2.2
ip route 198.18.55.0/24 null0
interface ethernet 1/1/9
 port-name PRESERVE
end`
	prefix, gateway := netip.MustParsePrefix("198.18.53.0/24"), netip.MustParseAddr("192.0.2.2")
	for _, tc := range []struct {
		name, after string
		allowed     bool
	}{
		{"distance", strings.Replace(before, "distance 200", "distance 202", 1), true},
		{"deletion", strings.Replace(before, "ip route 198.18.53.0/24 192.0.2.2 distance 200\n", "", 1), true},
		{"neighbor option", strings.Replace(before, "name KEEP", "name CHANGED", 1), false},
		{"new neighbor option", strings.Replace(before, "distance 201", "7 distance 201", 1), false},
		{"other VRF", strings.Replace(before, "vrf blue", "vrf red", 1), false},
		{"null route", strings.Replace(before, "ip route 198.18.55.0/24 null0\n", "", 1), false},
		{"unrelated interface", strings.Replace(before, "PRESERVE", "CHANGED", 1), false},
		{"new route", strings.Replace(before, "end", "ip route 203.0.113.0/24 192.0.2.9\nend", 1), false},
		{"unexpected owned options", strings.Replace(before, "distance 200", "distance 200 tag 53", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := configtest.Parse(t, tc.after).CheckIPv4RouteUpdate(configtest.Parse(t, before), prefix, gateway)
			if (err == nil) != tc.allowed {
				t.Fatalf("update error=%v; want allowed=%t", err, tc.allowed)
			}
		})
	}
	reordered := `ver 09.0.10k
ip route 198.18.55.0/24 null0
ip route 198.18.53.0/24 192.0.2.3 distance 201 name KEEP
ip route 198.18.53.0/24 192.0.2.2 distance 202
ip route vrf blue 198.18.54.0/24 192.0.2.2
interface ethernet 1/1/9
 port-name PRESERVE
end`
	if err := configtest.Parse(t, reordered).CheckIPv4RouteUpdate(configtest.Parse(t, before), prefix, gateway); err != nil {
		t.Fatal(err)
	}
}
