package acl

import (
	"maps"
	"slices"
	"testing"
)

func TestNativeIP(t *testing.T) {
	current, unowned, err := nativeIP(`ver 09.0.10kT213
ip access-list extended EDGE
 sequence 10 permit tcp 192.0.2.0 0.0.0.255 eq 0 host 198.51.100.1 range 3000 4000 dscp-matching 0 dscp-marking 0 internal-priority-marking 0
 sequence 20 permit tcp any range cadlock2 2000 any eq ssl
 sequence 30 deny ip any any
ipv6 access-list EDGE
 sequence 10 permit ipv6 any any log
mac access-list EDGE
 sequence 10 deny any any
interface ethernet 1/1/9
 ip access-group EDGE in
end`, ipv4ACL, "EDGE")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]ipRule{
		10: {Sequence: 10, Action: "permit", Source: "192.0.2.0/24", Destination: "198.51.100.1/32", Protocol: optionalInt{6, true}, SourcePort: portMatch{0, 0, true}, DestinationPort: portMatch{3000, 4000, true}, DSCP: optionalInt{0, true}, DSCPMark: optionalInt{0, true}, Priority: optionalInt{0, true}},
		20: {Sequence: 20, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{6, true}, SourcePort: portMatch{1000, 2000, true}, DestinationPort: portMatch{443, 443, true}},
		30: {Sequence: 30, Action: "deny", Source: "any", Destination: "any"},
	}
	if current == nil || current.Family != ipv4ACL || current.Name != "EDGE" || !maps.Equal(current.Rules, want) {
		t.Fatalf("native ACL = %+v", current)
	}
	if !slices.Equal(unowned, []string{"ver 09.0.10kT213", "ipv6 access-list EDGE", " sequence 10 permit ipv6 any any log", "mac access-list EDGE", " sequence 10 deny any any", "interface ethernet 1/1/9", " ip access-group EDGE in", "end"}) {
		t.Fatalf("unowned configuration = %q", unowned)
	}
}

func TestNativeIPOwnership(t *testing.T) {
	for name, rule := range map[string]string{
		"logging":                "sequence 10 permit ip any any log",
		"remark":                 "remark operator policy",
		"noncontiguous wildcard": "sequence 10 permit ip 192.0.2.0 0.255.0.255 any",
		"port comparison":        "sequence 10 permit tcp any any gt 80",
		"unknown service":        "sequence 10 permit tcp any any eq new-service",
		"TCP flags":              "sequence 10 permit tcp any any established",
		"ICMP type":              "sequence 10 permit icmp any any echo",
		"duplicate option":       "sequence 10 permit ip any any dscp-matching 1 dscp-matching 2",
		"truncated range":        "sequence 10 permit tcp any any range 1",
		"duplicate sequence":     "sequence 10 permit ip any any\n sequence 10 deny ip any any",
		"invalid action":         "sequence 10 allow ip any any",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := nativeIP("ip access-list extended EDGE\n "+rule+"\nend", ipv4ACL, "EDGE"); err == nil {
				t.Fatal("unrepresented native policy accepted")
			}
		})
	}
}

func TestNativeIPPresence(t *testing.T) {
	empty, _, err := nativeIP("ip access-list extended EDGE\nend", ipv4ACL, "EDGE")
	if err != nil || empty == nil || len(empty.Rules) != 0 {
		t.Fatalf("empty ACL = %+v, %v", empty, err)
	}
	absent, _, err := nativeIP("ipv6 access-list EDGE\n sequence 10 permit ipv6 any any\nend", ipv4ACL, "EDGE")
	if err != nil || absent != nil {
		t.Fatalf("absent ACL = %+v, %v", absent, err)
	}
	if _, _, err := nativeIP("ip access-list standard EDGE\n sequence 10 permit any\nend", ipv4ACL, "EDGE"); err == nil {
		t.Fatal("IPv4 namespace collision accepted")
	}
}

func TestNativeIPv6Protocol(t *testing.T) {
	current, _, err := nativeIP(`ipv6 access-list EDGE
 sequence 10 permit 0 any any
 sequence 20 deny ipv6 any any
 sequence 30 permit icmp 2001:db8:1::/64 2001:db8:2::/64
end`, ipv6ACL, "EDGE")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]ipRule{
		10: {Sequence: 10, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{0, true}},
		20: {Sequence: 20, Action: "deny", Source: "any", Destination: "any"},
		30: {Sequence: 30, Action: "permit", Source: "2001:db8:1::/64", Destination: "2001:db8:2::/64", Protocol: optionalInt{58, true}},
	}
	if current == nil || !maps.Equal(current.Rules, want) {
		t.Fatalf("IPv6 ACL = %+v", current)
	}
}

func TestNativeServices(t *testing.T) {
	current, _, err := nativeIP(`ip access-list extended EDGE
 sequence 10 permit udp any eq dns any eq ntp
 sequence 20 permit tcp any range exec login any range asf-rmcp cadlock2
 sequence 30 permit tcp any eq 1023 any eq 65535
end`, ipv4ACL, "EDGE")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]ipRule{
		10: {Sequence: 10, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{17, true}, SourcePort: portMatch{53, 53, true}, DestinationPort: portMatch{123, 123, true}},
		20: {Sequence: 20, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{6, true}, SourcePort: portMatch{512, 513, true}, DestinationPort: portMatch{623, 1000, true}},
		30: {Sequence: 30, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{6, true}, SourcePort: portMatch{1023, 1023, true}, DestinationPort: portMatch{65535, 65535, true}},
	}
	if current == nil || !maps.Equal(current.Rules, want) {
		t.Fatalf("native service matches = %+v", current)
	}
}

func TestNativeProtocolNames(t *testing.T) {
	for name, number := range map[string]int64{"ipencap": 4, "ipip": 94, "ahp": 51, "ipv6-icmp": 58, "st2": 5, "st": 118, "divert": 254, "253": 253} {
		t.Run(name, func(t *testing.T) {
			current, _, err := nativeIP("ip access-list extended 100\n sequence 10 permit "+name+" any any\nend", ipv4ACL, "100")
			if err != nil {
				t.Fatal(err)
			}
			if current == nil || current.Rules[10].Protocol != (optionalInt{number, true}) {
				t.Fatalf("native %s ACL = %+v", name, current)
			}
		})
	}
}

func TestNativeUDPTime(t *testing.T) {
	current, _, err := nativeIP("ip access-list extended EDGE\n sequence 10 permit udp any eq time any eq shell\nend", ipv4ACL, "EDGE")
	if err != nil {
		t.Fatal(err)
	}
	want := ipRule{Sequence: 10, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{17, true}, SourcePort: portMatch{37, 37, true}, DestinationPort: portMatch{514, 514, true}}
	if current == nil || current.Rules[10] != want {
		t.Fatalf("UDP service names = %+v", current)
	}
}

func TestNativeIPv6Logging(t *testing.T) {
	current, _, err := nativeIP(`ipv6 access-list EDGE
 sequence 10 permit ipv6 2001:db8:1::/64 2001:db8:2::/64 log
 sequence 20 deny ipv6 any any
end`, ipv6ACL, "EDGE")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]ipRule{
		10: {Sequence: 10, Action: "permit", Source: "2001:db8:1::/64", Destination: "2001:db8:2::/64", Log: true},
		20: {Sequence: 20, Action: "deny", Source: "any", Destination: "any"},
	}
	if current == nil || !maps.Equal(current.Rules, want) {
		t.Fatalf("IPv6 logging rules = %+v", current)
	}

	if _, _, err := nativeIP("ipv6 access-list EDGE\n sequence 10 deny ipv6 any any log log\nend", ipv6ACL, "EDGE"); err == nil {
		t.Fatal("duplicate logging accepted")
	}
}
