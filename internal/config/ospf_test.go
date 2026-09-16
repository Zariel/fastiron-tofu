package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestCapturedOSPFDelete(t *testing.T) {
	for _, tc := range []struct {
		name          string
		area, binding bool
	}{
		{"area-created", true, false},
		{"binding-created", false, true},
		{"binding-deleted", true, false},
		{"cleaned", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "ospf", tc.name+".conf"))
			if err != nil {
				t.Fatal(err)
			}
			d := configtest.Parse(t, string(raw))
			if err := d.CheckOSPFAreaDelete("0.0.0.53"); (err == nil) != tc.area {
				t.Fatalf("area deletion: %v; allowed=%v", err, tc.area)
			}
			if err := d.CheckOSPFBindingDelete("0.0.0.53", "ve 3053"); (err == nil) != tc.binding {
				t.Fatalf("binding deletion: %v; allowed=%v", err, tc.binding)
			}
			if err := d.CheckOSPFBindingDelete("0.0.0.0", "ve 5"); err == nil {
				t.Fatal("allowed deleting binding with independently configured network type")
			}
		})
	}
}

func TestOSPFAreaDelete(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"decimal", "router ospf\n area 53", true},
		{"dotted", "router ospf\n area 0.0.0.53", true},
		{"whitespace", "router\tospf\n area\t53", true},
		{"missing", "router ospf\n area 0", false},
		{"stub", "router ospf\n area 53\n area 53 stub", false},
		{"unknown option", "router ospf\n area 53 future-option", false},
		{"binding", "router ospf\n area 53\ninterface ve 3053\n ip ospf area 53", false},
		{"malformed binding", "router ospf\n area 53\ninterface ve 3053\n ip ospf area", false},
		{"unrelated binding", "router ospf\n area 53\ninterface ve 5\n ip ospf area 0", true},
		{"other router", "router bgp\n area 53", false},
		{"other VRF", "router ospf vrf blue\n area 53", false},
		{"other VRF options", "router ospf\n area 53\nrouter ospf vrf blue\n area 53 stub", true},
		{"repeated", "router ospf\n area 53\n area 0.0.0.53", false},
		{"missing identifier", "router ospf\n area", false},
		{"overflow", "router ospf\n area 4294967296", false},
		{"invalid address", "router ospf\n area 0.0.0.256", false},
		{"banner", "banner motd ^\nrouter ospf\n area 53\n^", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := configtest.Parse(t, "ver 09.0.10k\n"+tc.body+"\nend")
			if err := d.CheckOSPFAreaDelete("0.0.0.53"); (err == nil) != tc.allowed {
				t.Fatalf("delete: %v; allowed=%v", err, tc.allowed)
			}
		})
	}
}

func TestOSPFBindingDelete(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"decimal", " ip ospf area 53", true},
		{"dotted", " ip ospf area 0.0.0.53", true},
		{"whitespace", " ip\tospf\tarea\t53", true},
		{"unrelated IP settings", " ip address 192.0.2.1/24\n ip ospf area 53", true},
		{"missing", " ip address 192.0.2.1/24", false},
		{"different area", " ip ospf area 0", false},
		{"repeated", " ip ospf area 53\n ip ospf area 0.0.0.53", false},
		{"option", " ip ospf area 53\n ip ospf network point-to-point", false},
		{"unknown option", " ip ospf area 53\n ip ospf future-option", false},
		{"trailing option", " ip ospf area 53 future-option", false},
		{"malformed", " ip ospf area", false},
		{"other stanza", "interface ve 5\n ip ospf area 53", false},
		{"duplicate interface", " ip ospf area 53\ninterface ve 3053\n ip ospf area 53", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := configtest.Parse(t, "ver 09.0.10k\ninterface ve 3053\n"+tc.body+"\nend")
			if err := d.CheckOSPFBindingDelete("0.0.0.53", "ve 3053"); (err == nil) != tc.allowed {
				t.Fatalf("delete: %v; allowed=%v", err, tc.allowed)
			}
		})
	}
}
