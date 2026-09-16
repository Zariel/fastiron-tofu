package config_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestOSPFAreas(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       map[netip.Addr][]string
	}{
		{"empty", "", map[netip.Addr][]string{}},
		{"process only", "router ospf", map[netip.Addr][]string{}},
		{"areas and options", "router ospf\n area 0\n area 53\n area 53 range 198.51.100.0/24\ninterface ve 5\n ip ospf area 0\n ip ospf network point-to-point", map[netip.Addr][]string{netip.MustParseAddr("0.0.0.0"): {"ve 5"}, netip.MustParseAddr("0.0.0.53"): {}}},
		{"full range", "router ospf\n area 4294967295\ninterface ve 5\n ip ospf area 255.255.255.255", map[netip.Addr][]string{netip.MustParseAddr("255.255.255.255"): {"ve 5"}}},
		{"VRF isolation", "router ospf\n area 53\nrouter ospf vrf blue\n area 53\ninterface ve 5\n ip ospf area 53\n vrf forwarding blue", map[netip.Addr][]string{netip.MustParseAddr("0.0.0.53"): {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := configtest.Parse(t, "ver 09.0.10k\n"+tc.body+"\nend")
			got, err := d.OSPFAreas()
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("areas=%v error=%v; want=%v", got, err, tc.want)
			}
		})
	}
}

func TestOSPFAreasMalformed(t *testing.T) {
	for _, body := range []string{
		"router ospf\n area 53\n area 0.0.0.53",
		"router ospf\n area 4294967296",
		"router ospf\n area",
		"router ospf\n area 53\nrouter ospf",
		"router ospf unexpected\n area 53",
		"router ospf\n area 53\ninterface ve 5\n ip ospf area 53\n ip ospf area 53",
		"router ospf\n area 53\ninterface ve 5\n ip ospf area 54",
		"router ospf\n area 53\ninterface ve 5\n ip ospf area 53\n vrf forwarding",
	} {
		d := configtest.Parse(t, "ver 09.0.10k\n"+body+"\nend")
		if got, err := d.OSPFAreas(); err == nil {
			t.Errorf("accepted %q: %v", body, got)
		}
	}
}

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
		{"unrelated binding", "router ospf\n area 53\n area 0\ninterface ve 5\n ip ospf area 0", true},
		{"other router", "router bgp\n area 53", false},
		{"other VRF", "router ospf vrf blue\n area 53", false},
		{"other VRF options", "router ospf\n area 53\nrouter ospf vrf blue\n area 53 stub", true},
		{"other VRF binding", "router ospf\n area 53\nrouter ospf vrf blue\n area 53\ninterface ve 5\n ip ospf area 53\n vrf forwarding blue", true},
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
		{"different VRF", " ip ospf area 53\n vrf forwarding blue", false},
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

func TestCapturedOSPFAreas(t *testing.T) {
	for _, tc := range []struct {
		name, id   string
		interfaces []string
	}{
		{"area-created", "0.0.0.53", []string{}},
		{"binding-created", "0.0.0.53", []string{"ve 3053"}},
		{"binding-deleted", "0.0.0.53", []string{}},
		{"cli-area", "0.0.0.56", []string{}},
		{"routed-interfaces", "0.0.0.58", []string{"ethernet 1/1/9", "lag 58"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "ospf", tc.name+".conf"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := configtest.Parse(t, string(raw)).OSPFAreas()
			want := map[netip.Addr][]string{netip.MustParseAddr("0.0.0.0"): {"ve 5"}, netip.MustParseAddr(tc.id): tc.interfaces}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("areas=%v error=%v; want=%v", got, err, want)
			}
		})
	}
}
