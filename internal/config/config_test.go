package config

import (
	"slices"
	"strings"
	"testing"
)

func TestDocument(t *testing.T) {
	input := "show running-config\r\nCurrent configuration:\r\n!\r\nver 09.0.10k\r\n!\r\nrouter bgp\r\n local-as 65011\r\n address-family ipv4 unicast\r\n  network 192.0.2.0/24\r\n exit-address-family\r\ninterface ethernet 1/1/12\r\n trust dscp \r\nend\r\nswitch#"
	doc, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	want := "ver 09.0.10k\nrouter bgp\n local-as 65011\n address-family ipv4 unicast\n  network 192.0.2.0/24\n exit-address-family\ninterface ethernet 1/1/12\n trust dscp\nend"
	if doc.String() != want {
		t.Fatalf("configuration = %q", doc.String())
	}
	parents := []int{-1, -1, 1, 1, 3, 1, -1, 6, -1}
	for i, want := range parents {
		if doc.Commands[i].Parent != want {
			t.Fatalf("command %d parent = %d, want %d", i, doc.Commands[i].Parent, want)
		}
	}
}

func TestBanner(t *testing.T) {
	input := "ver 09.0.10k\nbanner motd $\nend\ninterface ethernet 1/1/12\n trust dscp\n!\n\nKeep trailing spaces  \n$\ninterface ethernet 1/1/12\n disable\nend"
	doc, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if doc.String() != input {
		t.Fatalf("banner changed: %q", doc.String())
	}
	state, err := doc.InterfacePolicy("ethernet 1/1/12", DSCPTrust)
	if err != nil || state.Enabled {
		t.Fatalf("banner interpreted as configuration: %+v, %v", state, err)
	}
	if !strings.Contains(strings.Join(state.Remaining, "\n"), "Keep trailing spaces  \n$") {
		t.Fatal("banner omitted from unowned configuration")
	}
}

func TestIncomplete(t *testing.T) {
	for _, input := range []string{"", "end", "ver 09.0.10k", "ver 09.0.10k\ninterface lag 11\n end", "ver 09.0.10k\nbanner motd $\nend\n"} {
		if _, err := Parse(input); err == nil {
			t.Fatalf("accepted incomplete output %q", input)
		}
	}
}

func TestPolicyScope(t *testing.T) {
	input := "ver 09.0.10k\ninterface ethernet 1/1/12\n unknown-context\n  trust dscp\n protected-port\n voice-vlan 53\n broadcast limit 200 kbps\ninterface lag 11\n trust dscp\nend"
	doc, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	dscp, err := doc.InterfacePolicy("ethernet 1/1/12", DSCPTrust)
	if err != nil || dscp.Enabled {
		t.Fatalf("nested command adopted: %+v, %v", dscp, err)
	}
	if strings.Join(dscp.Remaining, "\n") != strings.Replace(input, "interface ethernet 1/1/12\n", "", 1) {
		t.Fatal("unowned commands changed")
	}
	protection, err := doc.InterfacePolicy("ethernet 1/1/12", Protection)
	if err != nil || !protection.Enabled {
		t.Fatalf("protection: %+v, %v", protection, err)
	}
	voice, err := doc.InterfacePolicy("ethernet 1/1/12", VoiceVLAN)
	if err != nil || voice.VLAN != 53 {
		t.Fatalf("voice: %+v, %v", voice, err)
	}
	storm, err := doc.InterfacePolicy("ethernet 1/1/12", StormControl)
	if err != nil || storm.Unit != "kbps" || storm.Limits["broadcast"] != 200 {
		t.Fatalf("storm: %+v, %v", storm, err)
	}
}

func TestCommandFields(t *testing.T) {
	document, err := Parse("ver 09.0.10k\ninterface\tethernet 1/1/12\n\tport-name café uplink\nend")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"ver", "09.0.10k"}, {"interface", "ethernet", "1/1/12"}, {"port-name", "café", "uplink"}, {"end"}}
	for i, fields := range want {
		if !slices.Equal(document.Commands[i].Fields, fields) {
			t.Fatalf("command %d: fields = %q, want %q", i, document.Commands[i].Fields, fields)
		}
	}
}
