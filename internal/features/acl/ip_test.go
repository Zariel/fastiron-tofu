package acl

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIPPayload(t *testing.T) {
	// Explicit zeroes are filters and markings. Omitting them broadens a rule.
	config := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{
		30: {Sequence: 30, Action: "deny", Source: "any", Destination: "any"},
		10: {Sequence: 10, Action: "permit", Source: "192.0.2.0/24", Destination: "198.51.100.1/32", Protocol: optionalInt{6, true}, SourcePort: portMatch{0, 0, true}, DestinationPort: portMatch{3000, 4000, true}, DSCP: optionalInt{0, true}, DSCPMark: optionalInt{0, true}, Priority: optionalInt{0, true}},
	}}
	if err := validateIP(config); err != nil {
		t.Fatal(err)
	}
	want := `{"acl-sets":{"acl-set":[{"name":"EDGE","type":"ACL_IPV4","config":{"name":"EDGE","type":"ACL_IPV4"},"acl-entries":{"acl-entry":[
 {"sequence-id":10,"config":{"sequence-id":10},"ipv4":{"config":{"source-address":"192.0.2.0/24","destination-address":"198.51.100.1/32","protocol":6,"dscp":0,"dscp-marking":{"dscp-marking":0},"internal-priority-marking":{"internal-priority-marking":0}}},"transport":{"config":{"source-port":"0","destination-port":"3000..4000"}},"actions":{"config":{"forwarding-action":"ACCEPT"}}},
 {"sequence-id":30,"config":{"sequence-id":30},"ipv4":{"config":{"source-address":"0.0.0.0/0","destination-address":"0.0.0.0/0"}},"actions":{"config":{"forwarding-action":"DROP"}}}
 ]}}]}}`
	raw, err := json.Marshal(ipPayload(config))
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, wantJSON any
	if err := json.Unmarshal(raw, &gotJSON); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("payload = %s; want %s", raw, want)
	}
}

func TestIPValidation(t *testing.T) {
	for name, change := range map[string]func(*ipConfig){
		"wrong family":        func(p *ipConfig) { p.Family = 5 },
		"numeric overflow":    func(p *ipConfig) { p.Name = "999999999999999999999999999999" },
		"standard number":     func(p *ipConfig) { p.Name = "90" },
		"noncanonical number": func(p *ipConfig) { p.Name = "0100" },
		"IPv6 number":         func(p *ipConfig) { p.Family = ipv6ACL; p.Name = "100" },
		"sequence zero": func(p *ipConfig) {
			p.Rules = map[int64]ipRule{0: {Sequence: 0, Action: "deny", Source: "any", Destination: "any"}}
		},
		"sequence mismatch":    func(p *ipConfig) { r := p.Rules[10]; r.Sequence = 20; p.Rules[10] = r },
		"duplicate":            func(p *ipConfig) { r := p.Rules[10]; r.Sequence = 20; p.Rules[20] = r },
		"wrong address family": func(p *ipConfig) { r := p.Rules[10]; r.Source = "2001:db8::/64"; p.Rules[10] = r },
		"host bits":            func(p *ipConfig) { r := p.Rules[10]; r.Source = "192.0.2.1/24"; p.Rules[10] = r },
		"IPv4 protocol zero":   func(p *ipConfig) { r := p.Rules[10]; r.Protocol = optionalInt{0, true}; p.Rules[10] = r },
		"IPv4 logging":         func(p *ipConfig) { r := p.Rules[10]; r.Log = true; p.Rules[10] = r },
		"protocol 255":         func(p *ipConfig) { r := p.Rules[10]; r.Protocol = optionalInt{255, true}; p.Rules[10] = r },
		"SCTP port": func(p *ipConfig) {
			r := p.Rules[10]
			r.Protocol = optionalInt{132, true}
			r.SourcePort = portMatch{53, 53, true}
			p.Rules[10] = r
		},
		"reversed range": func(p *ipConfig) {
			r := p.Rules[10]
			r.Protocol = optionalInt{6, true}
			r.SourcePort = portMatch{20, 10, true}
			p.Rules[10] = r
		},
		"oversized port": func(p *ipConfig) {
			r := p.Rules[10]
			r.Protocol = optionalInt{17, true}
			r.DestinationPort = portMatch{65536, 65536, true}
			p.Rules[10] = r
		},
		"DSCP overflow":     func(p *ipConfig) { r := p.Rules[10]; r.DSCP = optionalInt{64, true}; p.Rules[10] = r },
		"priority overflow": func(p *ipConfig) { r := p.Rules[10]; r.Priority = optionalInt{8, true}; p.Rules[10] = r },
	} {
		t.Run(name, func(t *testing.T) {
			p := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{10: {Sequence: 10, Action: "permit", Source: "any", Destination: "any"}}}
			change(&p)
			if err := validateIP(p); err == nil {
				t.Fatal("unrepresentable ACL accepted")
			}
		})
	}
}

func TestIPv6ProtocolZero(t *testing.T) {
	p := ipConfig{Family: ipv6ACL, Name: "EDGE", Rules: map[int64]ipRule{10: {Sequence: 10, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{0, true}}}}
	if err := validateIP(p); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ipPayload(p))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"acl-sets":{"acl-set":[{"acl-entries":{"acl-entry":[{"actions":{"config":{"forwarding-action":"ACCEPT"}},"config":{"sequence-id":10},"ipv6":{"config":{"destination-address":"::/0","protocol":0,"source-address":"::/0"}},"sequence-id":10}]},"config":{"name":"EDGE","type":"ACL_IPV6"},"name":"EDGE","type":"ACL_IPV6"}]}}`
	if string(raw) != want {
		t.Fatalf("IPv6 protocol zero payload = %s", raw)
	}
}

func TestIPv6LogPayload(t *testing.T) {
	config := ipConfig{Family: ipv6ACL, Name: "EDGE", Rules: map[int64]ipRule{
		10: {Sequence: 10, Action: "permit", Source: "2001:db8:1::/64", Destination: "2001:db8:2::/64", Log: true},
		20: {Sequence: 20, Action: "deny", Source: "any", Destination: "any"},
	}}
	if err := validateIP(config); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ipPayload(config))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"acl-sets":{"acl-set":[{"acl-entries":{"acl-entry":[{"actions":{"config":{"forwarding-action":"ACCEPT","log-action":"openconfig-acl:LOG_SYSLOG"}},"config":{"sequence-id":10},"ipv6":{"config":{"destination-address":"2001:db8:2::/64","source-address":"2001:db8:1::/64"}},"sequence-id":10},{"actions":{"config":{"forwarding-action":"DROP"}},"config":{"sequence-id":20},"ipv6":{"config":{"destination-address":"::/0","source-address":"::/0"}},"sequence-id":20}]},"config":{"name":"EDGE","type":"ACL_IPV6"},"name":"EDGE","type":"ACL_IPV6"}]}}`
	if string(raw) != want {
		t.Fatalf("IPv6 logging payload = %s", raw)
	}
}
