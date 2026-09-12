package igmp

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

func TestGlobalReconcile(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		native, cached, desired settings
		failPatch, corrupt      bool
	}{
		{name: "enable", native: settings{Mode: "disabled", Version: 2}, desired: settings{Mode: "active", Version: 3}},
		{name: "disable", native: settings{Mode: "active", Version: 3}, cached: settings{Mode: "active", Version: 3}, desired: settings{Mode: "disabled", Version: 2}},
		{name: "native only reset", native: settings{Mode: "active", Version: 3}, desired: settings{Mode: "disabled", Version: 2}},
		{name: "stale values", native: settings{Mode: "disabled", Version: 2}, cached: settings{Mode: "passive", Version: 3}, desired: settings{Mode: "passive", Version: 3}},
		{name: "partial failure", native: settings{Mode: "disabled", Version: 2}, desired: settings{Mode: "active", Version: 3}, failPatch: true},
		{name: "unrelated change", native: settings{Mode: "disabled", Version: 2}, desired: settings{Mode: "active", Version: 2}, corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			native, cached := tc.native, tc.cached
			timer, failPatch := 127, tc.failPatch
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					lines := []string{"ver 09.0.10kT213"}
					if native.Mode != "disabled" {
						lines = append(lines, "ip multicast "+native.Mode)
					}
					if native.Version != 2 {
						lines = append(lines, fmt.Sprintf("ip multicast version %d", native.Version))
					}
					lines = append(lines, fmt.Sprintf("ip multicast query-interval %d", timer), "vlan 53 by port", " multicast passive", " multicast version 2", "end")
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
					json.NewEncoder(w).Encode(map[string]any{"icx-igmp-mld-snooping:global": map[string]any{"igmp": map[string]any{"config": cached}}})
					return
				}
				if r.Method == http.MethodDelete {
					changed := false
					if (strings.HasSuffix(r.URL.Path, "/querier-mode") || strings.HasSuffix(r.URL.Path, "/config")) && cached.Mode != "" {
						native.Mode = "disabled"
						cached.Mode = ""
						changed = true
					}
					if (strings.HasSuffix(r.URL.Path, "/version") || strings.HasSuffix(r.URL.Path, "/config")) && cached.Version != 0 {
						native.Version = 2
						cached.Version = 0
						changed = true
					}
					if !changed {
						w.WriteHeader(404)
						return
					}
					w.WriteHeader(204)
					return
				}
				if r.Method != http.MethodPatch {
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
					return
				}
				var body struct {
					Config settings `json:"config"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if body.Config.Mode != "" && body.Config.Mode != cached.Mode {
					native.Mode = body.Config.Mode
					cached.Mode = body.Config.Mode
					if native.Mode == "disabled" {
						native.Mode = "passive"
					}
				}
				if body.Config.Version != 0 && body.Config.Version != cached.Version {
					native.Version = body.Config.Version
					cached.Version = body.Config.Version
				}
				if tc.corrupt {
					timer = 200
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

			observed, err := applyGlobal(context.Background(), device, tc.desired)
			if tc.corrupt {
				if err == nil || !strings.Contains(err.Error(), "unrelated configuration") {
					t.Fatalf("missing preservation error: %v", err)
				}
				return
			}
			if tc.failPatch {
				mu.Lock()
				actual := native
				mu.Unlock()
				if err == nil || observed == nil || *observed != actual {
					t.Fatalf("partial state=%v native=%+v error=%v", observed, actual, err)
				}
				observed, err = applyGlobal(context.Background(), device, tc.desired)
			}
			if err != nil || observed == nil || *observed != tc.desired {
				t.Fatalf("observed=%v error=%v", observed, err)
			}
			mu.Lock()
			if native != tc.desired || timer != 127 {
				t.Errorf("native=%+v timer=%d", native, timer)
			}
			mu.Unlock()
			if _, err := applyGlobal(context.Background(), device, tc.desired); err != nil {
				t.Fatalf("repeat: %v", err)
			}
		})
	}
}
