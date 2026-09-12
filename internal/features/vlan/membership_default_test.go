package vlan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestMembershipDefault(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		defaultID, access, desired, wantAccess int64
		present, wantErr                       bool
	}{
		{"released VLAN 1", 4095, 4095, 1, 1, true, false},
		{"reset to selected default", 4095, 1, 1, 4095, false, false},
		{"occupied VLAN 1", 4095, 1, 53, 1, true, true},
		{"implicit default", 1, 1, 1, 1, true, true},
		{"implicit default removal", 1, 1, 1, 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			access := tc.access
			writes := 0
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return fmt.Sprintf("ver 09.0.10kT213\ndefault-vlan-id %d\nend", tc.defaultID)
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if strings.Contains(r.URL.Path, "/vlans/") {
					if r.Method != http.MethodGet {
						t.Error("membership changed VLAN existence")
						w.WriteHeader(405)
						return
					}
					fmt.Fprintf(w, `{"openconfig-network-instance:vlan":[{"vlan-id":%d,"config":{"vlan-id":%d,"name":"ORDINARY"}}]}`, tc.desired, tc.desired)
					return
				}
				switch r.Method {
				case http.MethodGet:
					fmt.Fprintf(w, `{"openconfig-vlan:switched-vlan":{"config":{"access-vlan":%d,"trunk-vlans":[55]}}}`, access)
					return
				case http.MethodPatch:
					var body struct {
						Port struct {
							Config switchport `json:"config"`
						} `json:"openconfig-vlan:switched-vlan"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					access = body.Port.Config.Access
				case http.MethodDelete:
					if !strings.HasSuffix(r.URL.Path, "/config/access-vlan") {
						t.Errorf("unexpected delete %s", r.URL.Path)
						w.WriteHeader(400)
						return
					}
					access = tc.defaultID
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
					return
				}
				writes++
				w.WriteHeader(204)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "manual",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = applyMembership(context.Background(), device, membership{VLANID: tc.desired, Interface: "ethernet 1/1/2", Tagging: "untagged"}, tc.present)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v; want error=%v", err, tc.wantErr)
			}
			mu.Lock()
			defer mu.Unlock()
			if access != tc.wantAccess {
				t.Errorf("access VLAN=%d; want %d", access, tc.wantAccess)
			}
			if tc.wantErr && writes != 0 {
				t.Error("ownership refusal changed membership")
			}
		})
	}
}
