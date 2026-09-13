package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestFeatureGrammar(t *testing.T) {
	document, err := Parse(`ver 09.0.10k
 default-vlan-id 900
ip multicast passive
ip multicast version 3
default-vlan-id 53
vlan 53 name USERS by port
 multicast active
 multicast version 2
ip access-list standard STAFF
ip access-list extended SERVICES
ipv6 access-list V6
mac access-list L2
interface ethernet 1/1/12
 multicast limit 123 kbps threshold 456 action port-shutdown 3
 unknown-unicast limit 789 kbps log
interface ve 53
 port-name café  transit
 disable
end`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := document.DefaultVLAN()
	if err != nil || id != 53 {
		t.Fatalf("default VLAN = %d, %v", id, err)
	}
	vlans, err := document.VLANs()
	if err != nil || len(vlans) != 1 {
		t.Fatalf("VLANs = %v, %v", vlans, err)
	}
	header, exists := vlans[53]
	if !exists {
		t.Fatal("VLAN 53 missing")
	}
	global, err := document.IGMP(-1)
	if err != nil || global.Mode != "passive" || global.Version != 3 {
		t.Fatalf("global IGMP = %+v, %v", global, err)
	}
	local, err := document.IGMP(header)
	if err != nil || local.Mode != "active" || local.Version != 2 {
		t.Fatalf("VLAN IGMP = %+v, %v", local, err)
	}
	acls, err := document.ACLs()
	want := []ACL{
		{ID: "ip access-list standard STAFF", Kind: "ipv4_standard", Name: "STAFF"},
		{ID: "ip access-list extended SERVICES", Kind: "ipv4_extended", Name: "SERVICES"},
		{ID: "ipv6 access-list V6", Kind: "ipv6", Name: "V6"},
		{ID: "mac access-list L2", Kind: "mac", Name: "L2"},
	}
	if err != nil || !reflect.DeepEqual(acls, want) {
		t.Fatalf("ACLs = %+v, %v", acls, err)
	}
	storm, err := document.InterfacePolicy("ethernet 1/1/12", StormControl)
	if err != nil || storm.Unit != "kbps" || !storm.Options || !reflect.DeepEqual(storm.Limits, map[string]int64{"multicast": 123, "unknown-unicast": 789}) {
		t.Fatalf("storm = %+v, %v", storm, err)
	}
	ve, err := document.VE(53)
	if err != nil || !ve.Exists || ve.Enabled || ve.PortName != "café  transit" {
		t.Fatalf("VE = %+v, %v", ve, err)
	}
}

func TestMalformedGrammar(t *testing.T) {
	tests := []struct {
		name, prefix string
		commands     []string
		extract      func(*Document) error
	}{
		{"default VLAN", "", []string{"default-vlan-id", "default-vlan-id 53 extra", "default-vlan-id 4096", "default-vlan-id nope", "default-vlan-id 53\ndefault-vlan-id 54"}, func(d *Document) error { _, e := d.DefaultVLAN(); return e }},
		{"VLAN", "", []string{"vlan", "vlan invalid", "vlan 0", "vlan 99999999999999999999999999"}, func(d *Document) error { _, e := d.VLANs(); return e }},
		{"ACL", "", []string{"ip access-list", "ip access-list standard", "ip access-list standard A B", "ipv6 access-list A B", "mac access-list", "mac access-list L2\nmac\taccess-list L2"}, func(d *Document) error { _, e := d.ACLs(); return e }},
		{"IGMP", "", []string{"ip multicast", "ip multicast active extra", "ip multicast version", "ip multicast version 4", "ip multicast version 2 extra"}, func(d *Document) error { _, e := d.IGMP(-1); return e }},
		{"storm", "interface ethernet 1/1/12\n ", []string{"multicast limit invalid", "multicast limit", "multicast limit 3 mbps", "broadcast limit 0", "unknown-unicast limit 3 pps extra"}, func(d *Document) error { _, e := d.InterfacePolicy("ethernet 1/1/12", StormControl); return e }},
		{"DSCP", "interface ethernet 1/1/12\n ", []string{"trust dscp extra"}, func(d *Document) error { _, e := d.InterfacePolicy("ethernet 1/1/12", DSCPTrust); return e }},
		{"protection", "interface ethernet 1/1/12\n ", []string{"protected-port extra"}, func(d *Document) error { _, e := d.InterfacePolicy("ethernet 1/1/12", Protection); return e }},
		{"voice", "interface ethernet 1/1/12\n ", []string{"voice-vlan", "voice-vlan 0053", "voice-vlan 53 extra", "voice-vlan 4096"}, func(d *Document) error { _, e := d.InterfacePolicy("ethernet 1/1/12", VoiceVLAN); return e }},
		{"VE", "interface ve 53\n ", []string{"port-name", "disable extra"}, func(d *Document) error { _, e := d.VE(53); return e }},
		{"jumbo", "", []string{"jumbo extra"}, func(d *Document) error { _, _, e := d.Jumbo(); return e }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, command := range tt.commands {
				t.Run(command, func(t *testing.T) {
					document, err := Parse("ver 09.0.10k\n" + tt.prefix + command + "\nend")
					if err != nil {
						t.Fatal(err)
					}
					if err := tt.extract(document); err == nil {
						t.Fatal("malformed owned command accepted")
					}
				})
			}
		})
	}
}

func TestUnknownMulticast(t *testing.T) {
	input := "ver 09.0.10k\nip multicast future-option 42\ninterface ethernet 1/1/12\n multicast limit 123 pps\nend"
	document, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	state, err := document.IGMP(-1)
	if err != nil || state.Mode != "disabled" || state.Version != 2 || strings.Join(state.Remaining, "\n") != input {
		t.Fatalf("unknown configuration adopted: %+v, %v", state, err)
	}
}
