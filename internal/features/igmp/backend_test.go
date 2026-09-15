package igmp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestNative(t *testing.T) {
	configuration := "ver 09.0.10k\nip multicast active\nvlan 53 name MEDIA by port\n multicast disable-igmp-snoop\n multicast tracking\n multicast version 3\nvlan 54 by port\n multicast passive\nend"
	got, err := parse(configtest.Parse(t, configuration), 53)
	if err != nil {
		t.Fatal(err)
	}
	if got.settings != (settings{Mode: "disabled", Version: 3}) {
		t.Fatalf("overrides=%+v", got.settings)
	}
	want := "ver 09.0.10k\nip multicast active\nvlan 53 name MEDIA by port\n multicast tracking\nvlan 54 by port\n multicast passive\nend"
	if strings.Join(got.unowned, "\n") != want {
		t.Fatalf("unowned configuration: %q", got.unowned)
	}
	if _, err := parse(configtest.Parse(t, configuration), 55); !errors.Is(err, fastiron.ErrNotFound) {
		t.Fatalf("missing VLAN: %v", err)
	}
	if _, err := parse(configtest.Parse(t, strings.Replace(configuration, " multicast tracking", " multicast active", 1)), 53); err == nil {
		t.Fatal("accepted ambiguous modes")
	}
}

func TestReconcile(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		native, cached, desired settings
		failPatch, corrupt      bool
	}{
		{name: "create", desired: settings{Mode: "active", Version: 3}},
		{name: "cached desired value", native: settings{Mode: "disabled", Version: 2}, cached: settings{Mode: "active", Version: 3}, desired: settings{Mode: "active", Version: 3}},
		{name: "native only reset", native: settings{Mode: "disabled", Version: 3}},
		{name: "partial failure", native: settings{Mode: "active", Version: 3}, cached: settings{Mode: "active", Version: 3}, desired: settings{Mode: "passive", Version: 2}, failPatch: true},
		{name: "unrelated change", desired: settings{Mode: "active"}, corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			native, cached := tc.native, tc.cached
			tracking, failPatch := true, tc.failPatch
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					lines := []string{"ver 09.0.10kT213", "vlan 53 name MEDIA by port"}
					switch native.Mode {
					case "disabled":
						lines = append(lines, " multicast disable-igmp-snoop")
					case "active", "passive":
						lines = append(lines, " multicast "+native.Mode)
					}
					if tracking {
						lines = append(lines, " multicast tracking")
					}
					if native.Version != 0 {
						lines = append(lines, fmt.Sprintf(" multicast version %d", native.Version))
					}
					lines = append(lines, "vlan 54 name OTHER by port", " multicast active", "router bgp", " local-as 65011", "end")
					return strings.Join(lines, "\n")
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					json.NewEncoder(w).Encode(map[string]any{"icx-igmp-mld-snooping:vlans": map[string]any{"vlan": []any{map[string]any{"vlan-id": 53, "proto": map[string]any{"vlan-id": 53, "igmp": map[string]any{"config": cached}}}}}})
					return
				}
				if r.Method == http.MethodDelete {
					mode := strings.HasSuffix(r.URL.Path, "/querier-mode") || strings.HasSuffix(r.URL.Path, "/config")
					version := strings.HasSuffix(r.URL.Path, "/version") || strings.HasSuffix(r.URL.Path, "/config")
					changed := false
					if mode && cached.Mode != "" {
						cached.Mode = ""
						changed = true
						if native.Mode != "disabled" {
							native.Mode = ""
						}
					}
					if version && cached.Version != 0 {
						cached.Version = 0
						native.Version = 0
						changed = true
					}
					if !changed {
						w.WriteHeader(404)
						return
					}
					w.WriteHeader(204)
					return
				}
				if r.Method != http.MethodPatch && r.Method != http.MethodPost {
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
					return
				}
				var body struct {
					VLANs struct {
						VLAN []vlanEntry `json:"vlan"`
					} `json:"icx-igmp-mld-snooping:vlans"`
					VLAN []vlanEntry `json:"vlan"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				entries := body.VLANs.VLAN
				if r.Method == http.MethodPost {
					entries = body.VLAN
				}
				if len(entries) != 1 || entries[0].ID != 53 || entries[0].Proto.ID != 53 || entries[0].Proto.IGMP.Config == nil {
					t.Error("invalid target")
					w.WriteHeader(400)
					return
				}
				value := *entries[0].Proto.IGMP.Config
				// Identical RESTCONF values do not execute the native callback.
				if value.Mode != "" && value.Mode != cached.Mode {
					native.Mode = value.Mode
					cached.Mode = value.Mode
				}
				if value.Version != 0 && value.Version != cached.Version {
					native.Version = value.Version
					cached.Version = value.Version
				}
				if tc.corrupt {
					tracking = false
				}
				if failPatch {
					failPatch = false
					w.WriteHeader(500)
					return
				}
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

			observed, err := apply(context.Background(), device, 53, tc.desired, true)
			if tc.corrupt {
				if err == nil || !strings.Contains(err.Error(), "unrelated configuration") {
					t.Fatalf("missing preservation error: %v", err)
				}
				return
			}
			if tc.failPatch {
				if err == nil || observed == nil || observed.Mode != "passive" {
					t.Fatalf("partial state=%v error=%v", observed, err)
				}
				observed, err = apply(context.Background(), device, 53, tc.desired, true)
			}
			if err != nil || observed == nil || *observed != tc.desired {
				t.Fatalf("observed=%v error=%v", observed, err)
			}
			mu.Lock()
			if native != tc.desired || !tracking {
				t.Errorf("native=%+v tracking=%v", native, tracking)
			}
			mu.Unlock()
			if _, err := apply(context.Background(), device, 53, tc.desired, true); err != nil {
				t.Fatalf("repeat: %v", err)
			}
		})
	}
}
