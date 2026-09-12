package acl

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestMACREST(t *testing.T) {
	const response = `{"openconfig-acl:acl-sets":{"acl-set":[{
		"name":"EDGE","type":"openconfig-acl:ACL_L2","acl-entries":{"acl-entry":[
			{"sequence-id":89,"config":{"sequence-id":89},
			 "l2":{"config":{"source-mac":"any","source-mac-mask":"any","destination-mac":"any","destination-mac-mask":"any","ethertype":2048}},
			 "actions":{"config":{"forwarding-action":"openconfig-acl:ACCEPT"}}},
			{"sequence-id":37,"config":{"sequence-id":37},
			 "l2":{"config":{"source-mac":"any","source-mac-mask":"any","destination-mac":"any","destination-mac-mask":"any","ethertype":2054}},
			 "actions":{"config":{"forwarding-action":"openconfig-acl:DROP"}}}
		]}
	}]}}`
	current := &macConfig{Name: "EDGE", Rules: []macRule{
		{Action: "deny", EtherType: optionalInt{Value: 2054, Present: true}},
		{Action: "permit", EtherType: optionalInt{Value: 2048, Present: true}},
	}}
	logged := &macConfig{Name: "EDGE", Rules: []macRule{
		{Action: "deny", EtherType: optionalInt{Value: 2054, Present: true}},
		{Action: "permit", EtherType: optionalInt{Value: 2048, Present: true}, Log: true},
	}}
	for name, tc := range map[string]struct {
		response string
		current  *macConfig
		wantErr  bool
	}{
		"unordered response":           {response, current, false},
		"logging omitted after reload": {response, logged, false},
		"explicit logging disagreement": {strings.Replace(response, `"forwarding-action":"openconfig-acl:ACCEPT"`,
			`"forwarding-action":"openconfig-acl:ACCEPT","log-action":"openconfig-acl:LOG_NONE"`, 1), logged, true},
		"unexpected logging": {strings.Replace(response, `"forwarding-action":"openconfig-acl:ACCEPT"`,
			`"forwarding-action":"openconfig-acl:ACCEPT","log-action":"openconfig-acl:LOG_SYSLOG"`, 1), current, true},
		"wrong rule order": {response, &macConfig{Name: "EDGE", Rules: []macRule{
			{Action: "permit", EtherType: optionalInt{Value: 2048, Present: true}},
			{Action: "deny", EtherType: optionalInt{Value: 2054, Present: true}},
		}}, true},
		"stale match":        {strings.ReplaceAll(response, "2048", "34525"), current, true},
		"stale action":       {strings.ReplaceAll(response, "ACCEPT", "DROP"), current, true},
		"duplicate ID":       {strings.ReplaceAll(response, "89", "37"), current, true},
		"inconsistent ID":    {strings.Replace(response, `"sequence-id":89`, `"sequence-id":88`, 1), current, true},
		"missing mask":       {strings.Replace(response, `"source-mac-mask":"any",`, "", 1), current, true},
		"missing collection": {`{}`, current, true},
		"missing parent":     {`{"openconfig-acl:acl-sets":{"acl-set":[]}}`, current, true},
		"stale parent":       {response, nil, true},
		"other family":       {strings.ReplaceAll(response, "ACL_L2", "ACL_IPV6"), nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			var body any
			if err := json.Unmarshal([]byte(tc.response), &body); err != nil {
				t.Fatal(err)
			}
			s := &standardSwitch{running: absentStandard + "end", restACLs: body}
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("REST/native comparison attempted a write")
				w.WriteHeader(http.StatusInternalServerError)
			})

			entries, err := macEntries(context.Background(), device, "EDGE", tc.current)
			if (err != nil) != tc.wantErr {
				t.Fatalf("REST comparison: entries=%+v error=%v", entries, err)
			}
			if tc.wantErr {
				return
			}
			if tc.current == nil {
				if len(entries) != 0 {
					t.Fatal("adopted REST entries from another ACL family")
				}
				return
			}
			if len(entries) != 2 || entries[0].ID != 37 || entries[1].ID != 89 || entries[0].Rule != tc.current.Rules[0] || entries[1].Rule != tc.current.Rules[1] {
				t.Fatalf("REST IDs do not follow native rule order: %+v", entries)
			}
		})
	}
}
