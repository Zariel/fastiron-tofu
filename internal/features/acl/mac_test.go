package acl

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestNativeMAC(t *testing.T) {
	config, unowned, err := nativeMAC(`ver 09.0.10kT213
ip access-list extended EDGE
 sequence 10 permit ip any any
mac access-list EDGE
 permit 0200.0000.0001 ff00.ff00.ff00 0200.0000.0002 ffff.ffff.ffff ether-type 86dd log
 deny any any
interface ethernet 1/1/9
 mac access-group EDGE in
end`, "EDGE")
	if err != nil {
		t.Fatal(err)
	}
	want := []macRule{
		{
			Action: "permit", Log: true, EtherType: optionalInt{Value: 34525, Present: true},
			Source:      macMatch{Address: macAddress{2, 0, 0, 0, 0, 1}, Mask: macAddress{255, 0, 255, 0, 255, 0}},
			Destination: macMatch{Address: macAddress{2, 0, 0, 0, 0, 2}, Mask: macAddress{255, 255, 255, 255, 255, 255}},
		},
		{Action: "deny"},
	}
	if config == nil || config.Name != "EDGE" || !slices.Equal(config.Rules, want) {
		t.Fatalf("native rules = %+v", config)
	}
	if !slices.Equal(unowned, []string{
		"ver 09.0.10kT213", "ip access-list extended EDGE", " sequence 10 permit ip any any",
		"interface ethernet 1/1/9", " mac access-group EDGE in", "end",
	}) {
		t.Fatalf("unowned configuration = %q", unowned)
	}
}

func TestNativeMACUnsupported(t *testing.T) {
	for _, rule := range []string{
		"remark unmanaged", "enable accounting", "permit any any mirror", "permit any any log log",
		"permit any any ether-type 0500", "permit any any ether-type 0800 ether-type 0806",
		"permit any", "permit invalid ffff.ffff.ffff any",
		"permit 02:00:00:00:00:00:00:01 ffff.ffff.ffff any",
	} {
		t.Run(rule, func(t *testing.T) {
			if _, _, err := nativeMAC("mac access-list EDGE\n "+rule+"\nend", "EDGE"); err == nil {
				t.Fatal("unrepresented native setting accepted")
			}
		})
	}
}

func TestMACPayload(t *testing.T) {
	entries := []macEntry{
		{ID: 37, Rule: macRule{
			Action: "permit", EtherType: optionalInt{Value: 34525, Present: true}, Log: true,
			Source: macMatch{Address: macAddress{2, 0, 0, 0, 0, 1}, Mask: macAddress{255, 0, 255, 0, 255, 0}},
		}},
		{ID: 89, Rule: macRule{Action: "deny"}},
	}
	wantJSON := `{"acl-sets":{"acl-set":[{
		"name":"EDGE","type":"ACL_L2","config":{"name":"EDGE","type":"ACL_L2"},
		"acl-entries":{"acl-entry":[
			{"sequence-id":37,"config":{"sequence-id":37},
			 "l2":{"config":{"source-mac":"02:00:00:00:00:01","source-mac-mask":"ff:00:ff:00:ff:00","destination-mac":"00:00:00:00:00:00","destination-mac-mask":"00:00:00:00:00:00","ethertype":34525}},
			 "actions":{"config":{"forwarding-action":"ACCEPT","log-action":"openconfig-acl:LOG_SYSLOG"}}},
			{"sequence-id":89,"config":{"sequence-id":89},
			 "l2":{"config":{"source-mac":"00:00:00:00:00:00","source-mac-mask":"00:00:00:00:00:00","destination-mac":"00:00:00:00:00:00","destination-mac-mask":"00:00:00:00:00:00"}},
			 "actions":{"config":{"forwarding-action":"DROP"}}}
		]}
	}]}}`
	data, err := json.Marshal(macPayload("EDGE", entries))
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MAC payload = %s", data)
	}
}
