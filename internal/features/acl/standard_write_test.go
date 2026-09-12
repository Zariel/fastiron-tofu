package acl

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

const (
	neighbor          = "ip access-list standard 91\n sequence 10 permit 203.0.113.0 0.0.0.255\ninterface ethernet 1/1/10\n ip access-group 91 in\nend"
	emptyStandard     = "ver 09.0.10kT213\nip access-list standard 90\n"
	populatedStandard = emptyStandard + " sequence 10 permit 192.0.2.0 0.0.0.255\n"
	absentStandard    = "ver 09.0.10kT213\n"
)

type standardSwitch struct {
	mu               sync.Mutex
	running, startup string
	writes, saves    int
	failSave         bool
	restACLs         any
}

func (s *standardSwitch) device(t *testing.T, handler http.HandlerFunc) *fastiron.Device {
	t.Helper()
	s.startup = s.running
	server := testswitch.New(t, func(command string) string {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return s.running
		case "show configuration":
			return s.startup
		case "write memory":
			if s.failSave {
				return "% Error writing startup-config"
			}
			s.saves++
			s.startup = s.running
			return "Write startup-config done."
		default:
			return "% Invalid input"
		}
	})
	server.HandleFunc("/restconf/data/acl/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Method == http.MethodGet {
			response := s.restACLs
			if response == nil {
				response = fixtureACLs(s.running)
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Error(err)
			}
			return
		}
		s.writes++
		handler(w, r)
	})
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.REST.Certificate().Raw})
	device, err := fastiron.New(fastiron.Config{
		Host: server.REST.URL, Transport: "restconf", Persistence: "after_each_write",
		RESTCONF: &restconf.Config{URL: server.REST.URL + "/restconf/data", Username: "automation", Password: "test", CA: string(ca), Timeout: time.Second},
		SSH:      &ssh.Config{Address: server.SSHAddress, Username: "automation", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func fixtureACLs(running string) map[string]any {
	sets := []any{}
	blocks := regexp.MustCompile(`(?m)^ip access-list extended ([^\n]+)\n((?: [^\n]*\n)*)`)
	sequences := regexp.MustCompile(`(?m)^ sequence (\d+) `)
	for _, block := range blocks.FindAllStringSubmatch(running, -1) {
		entries := []any{}
		for _, match := range sequences.FindAllStringSubmatch(block[2], -1) {
			sequence, _ := strconv.ParseInt(match[1], 10, 64)
			entries = append(entries, map[string]int64{"sequence-id": sequence})
		}
		sets = append(sets, map[string]any{"name": block[1], "type": "openconfig-acl:ACL_IPV4", "acl-entries": map[string]any{"acl-entry": entries}})
	}
	return map[string]any{"openconfig-acl:acl-sets": map[string]any{"acl-set": sets}}
}

func TestStandardPartialWrite(t *testing.T) {
	s := &standardSwitch{running: absentStandard + neighbor}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
		}
		s.running = populatedStandard + neighbor
		http.Error(w, "failed after applying", 500)
	})
	desired := standardConfig{Name: "90", Rules: map[int64]standardRule{10: {10, "permit", "192.0.2.0/24"}}}
	observed, err := applyStandard(context.Background(), device, desired)
	if err == nil || !strings.Contains(err.Error(), "500") || observed == nil {
		t.Fatalf("partial write: observed=%+v err=%v", observed, err)
	}
	if s.startup != absentStandard+neighbor || s.saves != 0 {
		t.Fatal("failed write saved configuration")
	}

	observed, err = applyStandard(context.Background(), device, desired)
	if err != nil || observed == nil {
		t.Fatalf("retry: observed=%+v err=%v", observed, err)
	}
	if s.writes != 1 || s.saves != 1 || s.startup != populatedStandard+neighbor {
		t.Fatalf("retry: writes=%d saves=%d startup=%q", s.writes, s.saves, s.startup)
	}
}

func TestStandardDeleteVerification(t *testing.T) {
	for name, tc := range map[string]struct{ running, after, message string }{
		"bound":            {populatedStandard + "interface ethernet 1/1/9\n ip access-group 90 in\n" + neighbor, "", "remove native ACL references"},
		"false absence":    {populatedStandard + neighbor, populatedStandard + neighbor, "remains in native configuration"},
		"neighbor changed": {populatedStandard + neighbor, absentStandard + "end", "changed unrelated native configuration"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &standardSwitch{running: tc.running}
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
				s.running = tc.after
				w.WriteHeader(http.StatusNotFound)
			})
			err := deleteStandard(context.Background(), device, "90")
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("delete: %v", err)
			}
			if s.saves != 0 || s.startup != tc.running {
				t.Fatal("unverified deletion saved configuration")
			}
			if name == "bound" && s.writes != 0 {
				t.Fatal("bound deletion attempted a mutation")
			}
		})
	}
}

func TestStandardEmpty(t *testing.T) {
	s := &standardSwitch{running: absentStandard + neighbor}
	device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if r.URL.Path != "/restconf/data/acl/acl-sets/acl-set/90/ACL_IPV4/acl-entries/acl-entry/1" {
				t.Errorf("delete path %s", r.URL.Path)
			}
			s.running = emptyStandard + neighbor
			w.WriteHeader(204)
			return
		}
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		// The initial owned rule must deny traffic; it must never reach startup.
		var want map[string]any
		if err := json.Unmarshal([]byte(`{"acl-sets":{"acl-set":[{"name":"90","type":"ACL_IPV4","standard":true,"config":{"name":"90","type":"ACL_IPV4","standard":true},"acl-entries":{"acl-entry":[{"sequence-id":1,"config":{"sequence-id":1},"ipv4":{"config":{"source-address":"0.0.0.0/0"}},"actions":{"config":{"forwarding-action":"DROP"}}}]}}]}}`), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("seed payload=%v", body)
		}
		s.running = emptyStandard + " sequence 1 deny 0.0.0.0 255.255.255.255\n" + neighbor
		w.WriteHeader(204)
	})
	observed, err := applyStandard(context.Background(), device, standardConfig{Name: "90", Rules: map[int64]standardRule{}})
	if err != nil || observed == nil || len(observed.Rules) != 0 {
		t.Fatalf("empty ACL: observed=%+v err=%v", observed, err)
	}
	if s.saves != 1 || s.startup != emptyStandard+neighbor {
		t.Fatalf("empty ACL saved=%q, saves=%d", s.startup, s.saves)
	}
}

func TestStandardPayload(t *testing.T) {
	body, err := json.Marshal(standardPayload("90", map[int64]standardRule{
		20: {20, "deny", "198.51.100.10/32"},
		10: {10, "permit", "192.0.2.0/24"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"acl-sets":{"acl-set":[{
  "name":"90","type":"ACL_IPV4","standard":true,
  "config":{"name":"90","type":"ACL_IPV4","standard":true},
  "acl-entries":{"acl-entry":[
   {"sequence-id":10,"config":{"sequence-id":10},"ipv4":{"config":{"source-address":"192.0.2.0/24"}},"actions":{"config":{"forwarding-action":"ACCEPT"}}},
   {"sequence-id":20,"config":{"sequence-id":20},"ipv4":{"config":{"source-address":"198.51.100.10/32"}},"actions":{"config":{"forwarding-action":"DROP"}}}
  ]}
 }]}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload=%s", body)
	}
}

func TestStandardReferences(t *testing.T) {
	for name, tc := range map[string]struct {
		config string
		bound  bool
	}{
		"description":        {"interface ethernet 1/1/9\n port-name access-group 90\n", false},
		"different ACL":      {"interface ethernet 1/1/9\n ip access-group 190 in\n", false},
		"prefix list":        {"route-map example permit 10\n match ip address prefix-list 90\n", false},
		"route map":          {"route-map example permit 10\n match ip address 89 90\n", true},
		"IGMP":               {"interface ve 5\n ip igmp access-group 90\n", true},
		"multicast boundary": {"interface ve 5\n ip multicast-boundary 90\n", true},
		"PIM neighbor":       {"interface ve 5\n ip pim neighbor-filter 90\n", true},
		"join policy":        {"router pim\n jp-policy 192.0.2.1 90\n", true},
		"slow path":          {"router pim\n slow-path-forwarding filter 90\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			s := &standardSwitch{running: populatedStandard + tc.config + neighbor}
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
				s.running = absentStandard + tc.config + neighbor
				w.WriteHeader(204)
			})
			err := deleteStandard(context.Background(), device, "90")
			if !tc.bound {
				if err != nil || s.startup != absentStandard+tc.config+neighbor {
					t.Fatalf("unrelated configuration blocked deletion: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "remove native ACL references") {
				t.Fatalf("reference accepted: %v", err)
			}
			if s.writes != 0 || s.saves != 0 || s.startup != populatedStandard+tc.config+neighbor {
				t.Fatal("referenced ACL was changed")
			}
		})
	}
}
