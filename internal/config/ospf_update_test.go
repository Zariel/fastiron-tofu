package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestOSPFBindingUpdate(t *testing.T) {
	const before = `ver 09.0.10k
router ospf
 area 0
 area 53
 area 53 range 198.51.100.0/24
interface ve 5
 ip ospf area 0
 ip ospf network point-to-point
interface ve 53
 port-name TRANSIT
 ip ospf area 53
ip route 192.0.2.0/24 192.0.2.1
end`
	removed := strings.Replace(before, " ip ospf area 53\n", "", 1)
	for _, tc := range []struct {
		name, after string
		allowed     bool
	}{
		{"unchanged", before, true},
		{"removed", removed, true},
		{"neighbor options", strings.Replace(removed, "point-to-point", "broadcast", 1), false},
		{"area options", strings.Replace(removed, "198.51.100.0/24", "198.51.101.0/24", 1), false},
		{"interface name", strings.Replace(removed, "TRANSIT", "CHANGED", 1), false},
		{"moved setting", strings.Replace(removed, "interface ve 53\n", "", 1), false},
		{"unrelated route", strings.Replace(removed, "192.0.2.1", "192.0.2.2", 1), false},
		{"added option", strings.Replace(removed, " port-name TRANSIT", " ip ospf cost 50\n port-name TRANSIT", 1), false},
		{"wrong area", strings.Replace(before, " ip ospf area 53", " ip ospf area 0", 1), false},
		{"duplicate binding", strings.Replace(before, " ip ospf area 53", " ip ospf area 53\n ip ospf area 53", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := configtest.Parse(t, before)
			next := configtest.Parse(t, tc.after)
			if err := next.CheckOSPFBindingUpdate(old, "0.0.0.53", "ve 53"); (err == nil) != tc.allowed {
				t.Fatalf("update=%v; allowed=%v", err, tc.allowed)
			}
		})
	}
	// Empty interface stanzas may appear with the only binding and disappear on removal.
	plain := configtest.Parse(t, "ver 09.0.10k\nrouter ospf\n area 53\nend")
	bound := configtest.Parse(t, "ver 09.0.10k\nrouter ospf\n area 53\ninterface ve 53\n ip ospf area 53\nend")
	if err := bound.CheckOSPFBindingUpdate(plain, "0.0.0.53", "ve 53"); err != nil {
		t.Fatal(err)
	}
	if err := plain.CheckOSPFBindingUpdate(bound, "0.0.0.53", "ve 53"); err != nil {
		t.Fatal(err)
	}
}

func TestCapturedOSPFBindingUpdate(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("testdata", "ospf", name+".conf"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := configtest.Parse(t, read("area-created"))
	bound := configtest.Parse(t, read("binding-created"))
	removed := configtest.Parse(t, read("binding-deleted"))
	if err := bound.CheckOSPFBindingUpdate(before, "0.0.0.53", "ve 3053"); err != nil {
		t.Fatal(err)
	}
	if err := removed.CheckOSPFBindingUpdate(bound, "0.0.0.53", "ve 3053"); err != nil {
		t.Fatal(err)
	}
}
