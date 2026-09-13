package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMEDCaptures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "lldp-med", "*.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no MED captures")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(strings.TrimSuffix(file, ".conf") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want struct {
				Inventory []string
				Policies  map[string]map[string]MEDPolicy
			}
			if err := json.Unmarshal(expected, &want); err != nil {
				t.Fatal(err)
			}
			if len(want.Inventory) == 0 || want.Policies == nil {
				t.Fatal("fixture missing inventory or expectations")
			}
			document, err := Parse(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := document.MEDPolicies(want.Inventory)
			if err != nil || !reflect.DeepEqual(got, want.Policies) {
				t.Fatalf("policies=%+v error=%v; want %+v", got, err, want.Policies)
			}
		})
	}
}

func TestMEDSyntax(t *testing.T) {
	prefix := "lldp med network-policy application "
	for _, command := range []string{
		"voice untagged dscp", "unknown untagged dscp 0 ports all",
		"voice untagged priority 3 dscp 0 ports all", "voice tagged vlan 0 priority 1 dscp 0 ports all",
		"voice tagged vlan 4095 priority 1 dscp 0 ports all", "voice priority-tagged priority 8 dscp 0 ports all",
		"voice untagged dscp 64 ports all", "voice untagged dscp -1 ports all",
		"voice tagged vlan 999999999999999999999 priority 1 dscp 0 ports all",
		"voice priority-tagged priority 999999999999999999999 dscp 0 ports all",
		"voice tagged vlan 1 priority 1 dscp 0 ports ethernet 1/1/10 to 1/1/12",
		"voice untagged dscp 0 ports ethernet 1/1/11\n" + prefix + "voice untagged dscp 1 ports ethernet 1/1/11",
	} {
		t.Run(command, func(t *testing.T) {
			d, err := Parse("ver 09.0.10k\n" + prefix + command + "\nend")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := d.MEDPolicies([]string{"ethernet 1/1/11"}); err == nil {
				t.Fatal("accepted malformed, duplicate or incomplete policy")
			}
		})
	}
}

func TestMEDScope(t *testing.T) {
	unowned := "ver 09.0.10k\nlldp run\nbanner motd $\nlldp med network-policy application voice untagged dscp 0 ports all\n$\ninterface ethernet 1/1/11\n lldp med network-policy application voice untagged dscp 0 ports all\nend"
	d, err := Parse(unowned)
	if err != nil {
		t.Fatal(err)
	}
	policies, remaining, err := d.MEDPolicies([]string{"ethernet 1/1/11"})
	if err != nil || len(policies) != 0 || strings.Join(remaining, "\n") != unowned {
		t.Fatalf("unowned configuration adopted: %v, %v", policies, err)
	}
}

func TestMEDAllPorts(t *testing.T) {
	document, err := Parse("ver 09.0.10k\nlldp med network-policy application voice tagged vlan 4094 priority 7 dscp 63 ports all\nend")
	if err != nil {
		t.Fatal(err)
	}
	policies, remaining, err := document.MEDPolicies([]string{"ethernet 1/1/1", "ethernet 2/3/4"})
	want := map[string]map[string]MEDPolicy{
		"ethernet 1/1/1": {"voice": {Traffic: "tagged", VLAN: 4094, Priority: 7, DSCP: 63}},
		"ethernet 2/3/4": {"voice": {Traffic: "tagged", VLAN: 4094, Priority: 7, DSCP: 63}},
	}
	if err != nil || !reflect.DeepEqual(policies, want) || strings.Join(remaining, "\n") != "ver 09.0.10k\nend" {
		t.Fatalf("all ports: policies=%+v remaining=%v error=%v", policies, remaining, err)
	}
}
