package acl

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	emptyIP     = "ver 09.0.10kT213\nip access-list extended EDGE\n"
	populatedIP = emptyIP + " sequence 10 permit tcp any any eq ssl\n"
)

func TestIPPartialWrite(t *testing.T) {
	s := &standardSwitch{running: absentStandard + neighbor}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		s.running = populatedIP + neighbor
		http.Error(w, "failed after applying", 500)
	})
	desired := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{10: {Sequence: 10, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{6, true}, DestinationPort: portMatch{443, 443, true}}}}
	observed, err := applyIP(context.Background(), device, desired)
	if err == nil || !strings.Contains(err.Error(), "500") || observed == nil || observed.Rules[10].DestinationPort.First != 443 {
		t.Fatalf("partial write: observed=%+v error=%v", observed, err)
	}
	if s.saves != 0 || s.startup != absentStandard+neighbor {
		t.Fatal("failed write persisted configuration")
	}

	observed, err = applyIP(context.Background(), device, desired)
	if err != nil || observed == nil {
		t.Fatalf("retry: observed=%+v error=%v", observed, err)
	}
	if s.writes != 1 || s.saves != 1 || s.startup != populatedIP+neighbor {
		t.Fatalf("retry: writes=%d saves=%d startup=%q", s.writes, s.saves, s.startup)
	}
}

func TestIPWriteVerification(t *testing.T) {
	for name, after := range map[string]string{
		"silently dropped port": emptyIP + " sequence 10 permit tcp any any\n" + neighbor,
		"neighbor lost":         populatedIP + "end",
	} {
		t.Run(name, func(t *testing.T) {
			s := &standardSwitch{running: absentStandard + neighbor}
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) { s.running = after; w.WriteHeader(204) })
			desired := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{10: {Sequence: 10, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{6, true}, DestinationPort: portMatch{443, 443, true}}}}
			if _, err := applyIP(context.Background(), device, desired); err == nil {
				t.Fatal("HTTP success accepted without configuration convergence")
			}
			if s.saves != 0 || s.startup != absentStandard+neighbor {
				t.Fatal("unverified change persisted")
			}
		})
	}
}

func TestIPEmpty(t *testing.T) {
	s := &standardSwitch{running: absentStandard + neighbor}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			s.running = emptyIP + " sequence 1 deny ip any any\n" + neighbor
		case http.MethodDelete:
			if r.URL.Path != "/restconf/data/acl/acl-sets/acl-set/EDGE/ACL_IPV4/acl-entries/acl-entry/1" {
				t.Errorf("delete path = %s", r.URL.Path)
			}
			s.running = emptyIP + neighbor
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
		w.WriteHeader(204)
	})
	desired := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{}}
	observed, err := applyIP(context.Background(), device, desired)
	if err != nil || observed == nil || len(observed.Rules) != 0 {
		t.Fatalf("empty ACL: observed=%+v error=%v", observed, err)
	}
	if s.startup != emptyIP+neighbor {
		t.Fatalf("saved configuration = %s", s.startup)
	}
}

func TestIPDeleteVerification(t *testing.T) {
	for name, tc := range map[string]struct{ running, after, message string }{
		"bound":            {populatedIP + "interface ethernet 1/1/9\n ip access-group EDGE in\n" + neighbor, "", "remove native ACL references"},
		"false absence":    {populatedIP + neighbor, populatedIP + neighbor, "remains in native configuration"},
		"neighbor changed": {populatedIP + neighbor, absentStandard + "end", "changed unrelated native configuration"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &standardSwitch{running: tc.running}
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) { s.running = tc.after; w.WriteHeader(404) })
			err := deleteIP(context.Background(), device, ipv4ACL, "EDGE")
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("delete error = %v", err)
			}
			if s.saves != 0 || s.startup != tc.running {
				t.Fatal("unverified deletion persisted")
			}
			if name == "bound" && s.writes != 0 {
				t.Fatal("bound ACL deletion attempted a mutation")
			}
		})
	}
}

func TestIPDeleteSaveRetry(t *testing.T) {
	s := &standardSwitch{running: populatedIP + neighbor, failSave: true}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		s.running = absentStandard + neighbor
		w.WriteHeader(204)
	})
	if err := deleteIP(context.Background(), device, ipv4ACL, "EDGE"); err == nil {
		t.Fatal("save failure ignored")
	}
	if s.startup != populatedIP+neighbor {
		t.Fatal("failed save changed startup configuration")
	}

	s.failSave = false
	if err := deleteIP(context.Background(), device, ipv4ACL, "EDGE"); err != nil {
		t.Fatal(err)
	}
	if s.writes != 1 || s.startup != absentStandard+neighbor {
		t.Fatalf("retry: writes=%d startup=%q", s.writes, s.startup)
	}
}

func TestIPSequenceMove(t *testing.T) {
	s := &standardSwitch{running: populatedIP + neighbor}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/restconf/data/acl/acl-sets/acl-set/EDGE/ACL_IPV4/acl-entries/acl-entry/10" {
			s.running = emptyIP + neighbor
			w.WriteHeader(204)
			return
		}
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		// FastIron rejects a duplicate packet rule even at a different sequence.
		if strings.Contains(s.running, " sequence 10 permit tcp any any eq ssl\n") {
			http.Error(w, "duplicate filter", 400)
			return
		}
		s.running = emptyIP + " sequence 20 permit tcp any any eq ssl\n" + neighbor
		w.WriteHeader(204)
	})
	desired := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{20: {Sequence: 20, Action: "permit", Source: "any", Destination: "any", Protocol: optionalInt{6, true}, DestinationPort: portMatch{443, 443, true}}}}
	observed, err := applyIP(context.Background(), device, desired)
	if err != nil || observed == nil || len(observed.Rules) != 1 || observed.Rules[20] != desired.Rules[20] {
		t.Fatalf("move: observed=%+v error=%v", observed, err)
	}
	if s.startup != emptyIP+" sequence 20 permit tcp any any eq ssl\n"+neighbor {
		t.Fatalf("saved configuration = %s", s.startup)
	}
}

func TestIPRemovalPreservesOtherRules(t *testing.T) {
	before := emptyIP + " sequence 30 deny ip any any\n sequence 40 permit icmp any any\n" + neighbor
	s := &standardSwitch{running: before}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected request %s", r.Method)
		}
		s.running = emptyIP + " sequence 30 deny ip host 0.0.0.0 host 0.0.0.0 dscp-matching 0 dscp-marking 0 internal-priority-marking 0\n" + neighbor
		w.WriteHeader(204)
	})
	desired := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{30: {Sequence: 30, Action: "deny", Source: "any", Destination: "any"}}}
	observed, err := applyIP(context.Background(), device, desired)
	if err == nil || observed == nil {
		t.Fatalf("unintended rule change: observed=%+v error=%v", observed, err)
	}
	if observed.Rules[30].Source != "0.0.0.0/32" || !observed.Rules[30].DSCP.Present {
		t.Fatalf("observed rule lost the unintended change: %+v", observed.Rules[30])
	}
	if s.writes != 1 || s.saves != 0 || s.startup != before {
		t.Fatalf("unverified change persisted: writes=%d saves=%d", s.writes, s.saves)
	}
}

func TestIPStaleSequences(t *testing.T) {
	for name, body := range map[string]string{
		"missing native entry": `{"openconfig-acl:acl-sets":{"acl-set":[{"name":"EDGE","type":"ACL_IPV4","acl-entries":{}}]}}`,
		"extra cached entry":   `{"openconfig-acl:acl-sets":{"acl-set":[{"name":"EDGE","type":"ACL_IPV4","acl-entries":{"acl-entry":[{"sequence-id":10},{"sequence-id":20}]}}]}}`,
		"absent cached ACL":    `{"openconfig-acl:acl-sets":{}}`,
		"missing collection":   `{}`,
		"duplicate sequence":   `{"openconfig-acl:acl-sets":{"acl-set":[{"name":"EDGE","type":"ACL_IPV4","acl-entries":{"acl-entry":[{"sequence-id":10},{"sequence-id":10}]}}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := &standardSwitch{running: populatedIP + neighbor}
			s.restACLs = json.RawMessage(body)
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("out-of-sync ACL was mutated")
				w.WriteHeader(500)
			})
			desired := ipConfig{Family: ipv4ACL, Name: "EDGE", Rules: map[int64]ipRule{}}
			if _, err := applyIP(context.Background(), device, desired); err == nil {
				t.Fatal("stale RESTCONF view accepted for update")
			}
			if err := deleteIP(context.Background(), device, ipv4ACL, "EDGE"); err == nil {
				t.Fatal("stale RESTCONF view accepted for deletion")
			}
			if s.writes != 0 || s.saves != 0 || s.running != populatedIP+neighbor || s.startup != populatedIP+neighbor {
				t.Fatal("mismatch changed switch state")
			}
		})
	}
}
