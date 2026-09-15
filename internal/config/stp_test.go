package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSTPVLAN(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       *STPVLAN
		invalid    bool
	}{
		{name: "disabled", body: "vlan 53 name test by port\n port-name unrelated"},
		{name: "classic default", body: "vlan 53\n spanning-tree", want: &STPVLAN{53, "stp", 32768}},
		{name: "zero priority", body: "vlan 53\n spanning-tree priority 0", want: &STPVLAN{53, "stp", 0}},
		{name: "RSTP priority", body: "vlan 53\n spanning-tree 802-1w\n spanning-tree 802-1w priority 12345", want: &STPVLAN{53, "rstp", 12345}},
		{name: "priority before mode", body: "vlan 53\n spanning-tree 802-1w priority 65535\n spanning-tree 802-1w", want: &STPVLAN{53, "rstp", 65535}},
		{name: "neighbor settings", body: "vlan 52\n spanning-tree hello-time 3\nvlan 53\n spanning-tree\ninterface ethernet 1/1/12\n spanning-tree root-protect", want: &STPVLAN{53, "stp", 32768}},
		{name: "missing VLAN", body: "vlan 52\n spanning-tree"},
		{name: "mixed modes", body: "vlan 53\n spanning-tree\n spanning-tree 802-1w", invalid: true},
		{name: "repeated mode", body: "vlan 53\n spanning-tree\n spanning-tree", invalid: true},
		{name: "repeated priority", body: "vlan 53\n spanning-tree priority 1\n spanning-tree priority 2", invalid: true},
		{name: "repeated header", body: "vlan 53\n spanning-tree\nvlan 53\n spanning-tree", invalid: true},
		{name: "missing value", body: "vlan 53\n spanning-tree priority", invalid: true},
		{name: "negative priority", body: "vlan 53\n spanning-tree priority -1", invalid: true},
		{name: "priority range", body: "vlan 53\n spanning-tree priority 65536", invalid: true},
		{name: "overflow", body: "vlan 53\n spanning-tree priority 18446744073709551616", invalid: true},
		{name: "interface flag in VLAN", body: "vlan 53\n spanning-tree\n spanning-tree root-protect", invalid: true},
		{name: "extra timer", body: "vlan 53\n spanning-tree\n spanning-tree hello-time 3", invalid: true},
		{name: "RSTP extra timer", body: "vlan 53\n spanning-tree 802-1w\n spanning-tree 802-1w hello-time 3", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Parse("ver 09.0.10k\n" + tc.body + "\nend")
			if err != nil {
				t.Fatal(err)
			}
			got, err := d.STPVLAN(53)
			if (err != nil) != tc.invalid {
				t.Fatalf("policy=%+v error=%v", got, err)
			}
			if !tc.invalid && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("policy=%+v; want %+v", got, tc.want)
			}
		})
	}
}

func TestSTPCaptures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "stp", "*.conf"))
	if err != nil || len(files) == 0 {
		t.Fatalf("captures=%v error=%v", files, err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file[:len(file)-len(".conf")] + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want STPVLAN
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			document, err := Parse(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := document.STPVLAN(want.VLANID)
			if err != nil || got == nil || *got != want {
				t.Fatalf("policy=%+v error=%v; want %+v", got, err, want)
			}
		})
	}
}

func TestSTPInterfaces(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       map[string]STPInterface
		invalid    bool
	}{
		{name: "flags", body: "interface ethernet 1/1/12\n spanning-tree 802-1w admin-edge-port\n stp-bpdu-guard\n spanning-tree root-protect", want: map[string]STPInterface{"ethernet 1/1/12": {AdminEdge: true, BPDUGuard: true, RootGuard: true}}},
		{name: "explicit false", body: "interface ethernet 1/1/12\n no stp-bpdu-guard\n no spanning-tree root-protect\n no spanning-tree 802-1w admin-edge-port", want: map[string]STPInterface{"ethernet 1/1/12": {}}},
		{name: "independent ports", body: "interface ethernet 1/1/11\n spanning-tree root-protect\ninterface ethernet 1/1/12\n stp-bpdu-guard", want: map[string]STPInterface{"ethernet 1/1/11": {RootGuard: true}, "ethernet 1/1/12": {BPDUGuard: true}}},
		{name: "other scopes", body: "vlan 53\n spanning-tree 802-1w priority 12345\nrouter test\n interface ethernet 1/1/12\n  stp-bpdu-guard", want: map[string]STPInterface{}},
		{name: "unowned settings", body: "interface ethernet 1/1/12\n spanning-tree priority 128\n port-name test", want: map[string]STPInterface{}},
		{name: "extra edge arguments", body: "interface ethernet 1/1/12\n spanning-tree 802-1w admin-edge-port unexpected", invalid: true},
		{name: "extra root arguments", body: "interface ethernet 1/1/12\n spanning-tree root-protect unexpected", invalid: true},
		{name: "extra BPDU arguments", body: "interface ethernet 1/1/12\n stp-bpdu-guard unexpected", invalid: true},
		{name: "duplicate flag", body: "interface ethernet 1/1/12\n stp-bpdu-guard\n no stp-bpdu-guard", invalid: true},
		{name: "duplicate stanza", body: "interface ethernet 1/1/12\n stp-bpdu-guard\ninterface ethernet 1/1/12", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Parse("ver 09.0.10k\n" + tc.body + "\nend")
			if err != nil {
				t.Fatal(err)
			}
			got, err := d.STPInterfaces()
			if (err != nil) != tc.invalid {
				t.Fatalf("flags=%v error=%v", got, err)
			}
			if !tc.invalid && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("flags=%v; want %v", got, tc.want)
			}
		})
	}
}

func TestSTPInterfaceCaptures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "stp-interfaces", "*.conf"))
	if err != nil || len(files) == 0 {
		t.Fatalf("captures=%v error=%v", files, err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file[:len(file)-len(".conf")] + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want map[string]STPInterface
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			document, err := Parse(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := document.STPInterfaces()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("flags=%v error=%v; want %v", got, err, want)
			}
		})
	}
}
