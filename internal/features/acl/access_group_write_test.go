package acl

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"reflect"
	"slices"
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
	groupParents  = "ver 09.0.10kT213\nip access-list standard 90\n sequence 10 permit any\nip access-list standard 91\n sequence 10 deny host 192.0.2.1\n"
	groupNeighbor = "interface ethernet 1/1/10\n ip access-group 91 in\nend"
)

type groupSwitch struct {
	mu                                      sync.Mutex
	active, startup                         string
	rest                                    map[string]bool
	writes, saves                           int
	failPatch, failPrune, failSave, corrupt bool
	corruptOnPatch                          bool
	unbound                                 bool
}

func (s *groupSwitch) native() string {
	output := groupParents
	if s.active != "" {
		output += "interface ethernet 1/1/9\n ip access-group " + s.active + " in\n"
	}
	if s.corrupt {
		return output + "end"
	}
	return output + groupNeighbor
}

func (s *groupSwitch) device(t *testing.T) *fastiron.Device {
	t.Helper()
	if s.rest == nil {
		s.rest = map[string]bool{}
	}
	s.startup = s.native()
	server := testswitch.New(t, func(command string) string {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return s.native()
		case "show configuration":
			return s.startup
		case "write memory":
			if s.failSave {
				return "Error: failed to create startup-config"
			}
			s.startup = s.native()
			s.saves++
			return "Write startup-config done."
		default:
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /restconf/data/interfaces", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9"}},{"name":"lag 1","config":{"name":"lag 1"}}]}}`)
	})
	server.HandleFunc("GET /restconf/data/acl/interfaces", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		entries := []any{}
		for name := range s.rest {
			entries = append(entries, map[string]any{"set-name": name, "type": "openconfig-acl:ACL_IPV4", "config": map[string]string{"set-name": name, "type": "openconfig-acl:ACL_IPV4"}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"openconfig-acl:interfaces": map[string]any{"interface": []any{map[string]any{"id": "ethernet 1/1/9", "config": map[string]string{"id": "ethernet 1/1/9"}, "ingress-acl-sets": map[string]any{"ingress-acl-set": entries}}}}})
	})
	server.HandleFunc("PATCH /restconf/data/acl/interfaces", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.writes++
		type reference struct {
			Name string `json:"set-name"`
		}
		type bindingInterface struct {
			Ingress struct {
				Entries []reference `json:"ingress-acl-set"`
			} `json:"ingress-acl-sets"`
		}
		var body struct {
			Interfaces struct {
				Interface []bindingInterface `json:"interface"`
			} `json:"interfaces"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(body.Interfaces.Interface) != 1 || len(body.Interfaces.Interface[0].Ingress.Entries) != 1 {
			t.Error("missing binding payload")
			w.WriteHeader(400)
			return
		}
		s.active = body.Interfaces.Interface[0].Ingress.Entries[0].Name
		s.rest[s.active] = true
		if s.corruptOnPatch {
			s.corrupt = true
		}
		if s.failPatch {
			s.failPatch = false
			http.Error(w, "partial patch failure", 500)
			return
		}
		w.WriteHeader(204)
	})
	server.HandleFunc("DELETE /restconf/data/acl/interfaces/interface/{interface}/ingress-acl-sets/ingress-acl-set/{acl}/ACL_IPV4", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.writes++
		if r.PathValue("interface") != "ethernet 1/1/9" {
			t.Errorf("interface=%q", r.PathValue("interface"))
		}
		name := r.PathValue("acl")
		if name != s.active && s.failPrune {
			s.failPrune = false
			http.Error(w, "cleanup failed", 500)
			return
		}
		delete(s.rest, name)
		if name != s.active {
			w.WriteHeader(404)
			return
		}
		s.active = ""
		s.unbound = true
		w.WriteHeader(204)
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

func TestAccessGroupRetry(t *testing.T) {
	for name, flags := range map[string]struct{ patch, prune bool }{"partial patch": {true, false}, "stale cleanup": {false, true}} {
		t.Run(name, func(t *testing.T) {
			s := &groupSwitch{active: "90", rest: map[string]bool{"90": true}, failPatch: flags.patch, failPrune: flags.prune}
			device := s.device(t)
			k := accessGroupKey{"ethernet 1/1/9", "ip", "in"}
			view, err := applyAccessGroup(context.Background(), device, k, "91")
			if err == nil || !strings.Contains(err.Error(), "500") || view.ACL != "91" {
				t.Fatalf("partial replacement view=%+v err=%v", view, err)
			}
			if s.saves != 0 || !strings.Contains(s.startup, "interface ethernet 1/1/9\n ip access-group 90 in") {
				t.Fatal("incomplete replacement was saved")
			}

			view, err = applyAccessGroup(context.Background(), device, k, "91")
			if err != nil {
				t.Fatal(err)
			}
			if view.ACL != "91" || !slices.Equal(view.RESTACLs, []string{"91"}) || s.saves != 1 || s.unbound {
				t.Fatalf("retry view=%+v saves=%d unbound=%v", view, s.saves, s.unbound)
			}
			if s.startup != groupParents+"interface ethernet 1/1/9\n ip access-group 91 in\n"+groupNeighbor {
				t.Fatalf("startup=%q", s.startup)
			}
		})
	}
}

func TestAccessGroupStaleOnly(t *testing.T) {
	s := &groupSwitch{rest: map[string]bool{"90": true}}
	device := s.device(t)
	view, err := applyAccessGroup(context.Background(), device, accessGroupKey{"ethernet 1/1/9", "ip", "in"}, "")
	if err != nil || view.ACL != "" || len(view.RESTACLs) != 0 {
		t.Fatalf("stale deletion view=%+v err=%v", view, err)
	}
	if s.saves != 1 || s.startup != groupParents+groupNeighbor {
		t.Fatal("stale cleanup changed native configuration or was not saved")
	}
}

func TestAccessGroupMissingACL(t *testing.T) {
	s := &groupSwitch{active: "90", rest: map[string]bool{"90": true}}
	device := s.device(t)
	_, err := applyAccessGroup(context.Background(), device, accessGroupKey{"ethernet 1/1/9", "ip", "in"}, "92")
	if err == nil || !strings.Contains(err.Error(), "before binding") || s.writes != 0 || s.saves != 0 {
		t.Fatalf("missing ACL: err=%v writes=%d saves=%d", err, s.writes, s.saves)
	}
}

func TestAccessGroupPath(t *testing.T) {
	k := accessGroupKey{"ethernet 1/1/9", "ipv6", "out"}
	if got := k.endpoint("V6"); got != "/acl/interfaces/interface/ethernet%201%2F1%2F9/egress-acl-sets/egress-acl-set/V6/ACL_IPV6" {
		t.Fatalf("endpoint=%s", got)
	}
}

func TestAccessGroupNeighborVerification(t *testing.T) {
	s := &groupSwitch{active: "90", rest: map[string]bool{"90": true}, corruptOnPatch: true}
	device := s.device(t)
	_, err := applyAccessGroup(context.Background(), device, accessGroupKey{"ethernet 1/1/9", "ip", "in"}, "91")
	if err == nil || !strings.Contains(err.Error(), "unrelated native configuration") {
		t.Fatalf("neighbor change accepted: %v", err)
	}
	if s.saves != 0 || !strings.HasSuffix(s.startup, groupNeighbor) {
		t.Fatal("unverified configuration was saved")
	}
}

func TestAccessGroupPayload(t *testing.T) {
	body, err := json.Marshal((accessGroupKey{"lag 1", "ipv6", "out"}).payload("V6"))
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"interfaces":{"interface":[{
  "id":"lag 1","config":{"id":"lag 1"},
  "egress-acl-sets":{"egress-acl-set":[{
   "set-name":"V6","type":"ACL_IPV6","config":{"set-name":"V6","type":"ACL_IPV6"}
  }]}
 }]}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload=%s", body)
	}
}

func TestAccessGroupDeletedLAG(t *testing.T) {
	s := &groupSwitch{}
	device := s.device(t)
	_, err := applyAccessGroup(context.Background(), device, accessGroupKey{"lag 1", "ip", "in"}, "90")
	if err == nil || !strings.Contains(err.Error(), "interface lag 1") {
		t.Fatalf("deleted native LAG accepted: %v", err)
	}
	if s.writes != 0 || s.saves != 0 {
		t.Fatal("stale LAG inventory allowed a mutation")
	}
}

func TestAccessGroupMissingVLAN(t *testing.T) {
	s := &groupSwitch{}
	device := s.device(t)
	_, err := applyAccessGroup(context.Background(), device, accessGroupKey{"vlan 100", "ip", "in"}, "90")
	if err == nil || !strings.Contains(err.Error(), "interface vlan 100") {
		t.Fatalf("missing native VLAN accepted: %v", err)
	}
	if s.writes != 0 || s.saves != 0 {
		t.Fatal("missing VLAN allowed a mutation")
	}
}
