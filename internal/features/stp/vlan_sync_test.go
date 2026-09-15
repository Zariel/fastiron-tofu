package stp

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

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestVLANCache(t *testing.T) {
	for _, mode := range []string{"stp", "rstp"} {
		for _, tc := range []struct {
			name                         string
			absent, stuck, changed, noop bool
		}{
			{name: "priority"}, {name: "presence", absent: true}, {name: "timeout", stuck: true}, {name: "unowned drift", changed: true}, {name: "persistence retry", noop: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				var mu sync.Mutex
				present, savedPresent, cachedPresent := !tc.absent, !tc.absent, true
				priority, savedPriority, cachedPriority := int64(12345), int64(12345), int64(65535)
				reads, writes, saves := 0, 0, 0
				name, savedName := "test", "test"
				desired := vlan{VLANID: 53, Mode: mode, Priority: 65535}
				if tc.noop {
					desired.Priority = 12345
					savedPriority = 4444
				}
				native := func(enabled bool, p int64, label string) string {
					text := "ver 09.0.10k\nvlan 53 name " + label + " by port\n tagged ethe 1/1/12\n"
					if enabled {
						if mode == "rstp" {
							text += " spanning-tree 802-1w\n spanning-tree 802-1w priority " + fmt.Sprint(p) + "\n"
						} else {
							text += " spanning-tree priority " + fmt.Sprint(p) + "\n"
						}
					}
					return text + "interface ethernet 1/1/11\n stp-bpdu-guard\nend"
				}
				server := testswitch.New(t, func(command string) string {
					mu.Lock()
					defer mu.Unlock()
					switch command {
					case "skip-page-display":
						return ""
					case "show version":
						return "SW: Version 09.0.10kT213"
					case "show running-config":
						return native(present, priority, name)
					case "show configuration":
						return native(savedPresent, savedPriority, savedName)
					case "write memory":
						savedPresent, savedPriority, savedName = present, priority, name
						saves++
						return "Write startup-config done."
					default:
						t.Errorf("unexpected command %q", command)
						return "% Invalid input"
					}
				})
				server.HandleFunc("/network-instances/network-instance=default-vrf/vlans/vlan=53", func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":53,"config":{"vlan-id":53,"name":"test"}}]}`)
				})
				container, field := "icx-openconfig-spanning-tree-aug:pvst", "pvst-priority"
				if mode == "rstp" {
					container, field = "rapid-pvst", "bridge-priority"
				}
				server.HandleFunc("/stp", func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					reads++
					if reads >= 3 && !tc.stuck {
						cachedPresent, cachedPriority = present, priority
						if tc.changed {
							name = "changed"
						}
					}
					entries := []any{}
					if cachedPresent {
						entries = append(entries, map[string]any{"vlan-id": 53, "config": map[string]any{"vlan-id": 53, field: cachedPriority}})
					}
					root := map[string]any{"rapid-pvst": map[string]any{}, "icx-openconfig-spanning-tree-aug:pvst": map[string]any{}}
					root[container] = map[string]any{"vlan": entries}
					json.NewEncoder(w).Encode(map[string]any{"openconfig-spanning-tree:stp": root})
				})
				server.HandleFunc(stpVLANPath(mode), func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					writes++
					var raw map[string]json.RawMessage
					if json.NewDecoder(r.Body).Decode(&raw) != nil {
						w.WriteHeader(400)
						return
					}
					if r.Method == http.MethodPatch {
						key := strings.TrimPrefix(container, "icx-openconfig-spanning-tree-aug:")
						var nested map[string]json.RawMessage
						if json.Unmarshal(raw[key], &nested) != nil {
							w.WriteHeader(400)
							return
						}
						raw = nested
					} else if r.Method != http.MethodPost {
						w.WriteHeader(405)
						return
					}
					var entries []struct {
						ID     int64            `json:"vlan-id"`
						Config map[string]int64 `json:"config"`
					}
					if json.Unmarshal(raw["vlan"], &entries) != nil || len(entries) != 1 || entries[0].ID != 53 || entries[0].Config["vlan-id"] != 53 {
						w.WriteHeader(400)
						return
					}
					value, ok := entries[0].Config[field]
					if !ok {
						w.WriteHeader(400)
						return
					}
					if r.Method == http.MethodPost && cachedPresent {
						w.WriteHeader(409)
						return
					}
					// Reproduce an acknowledged PATCH suppressed by its unchanged cached value.
					if !cachedPresent || value != cachedPriority {
						present, priority = true, value
					}
					cachedPresent, cachedPriority = true, value
					w.WriteHeader(204)
				})
				timeout := 2 * time.Second
				if tc.stuck {
					timeout = 100 * time.Millisecond
				}
				device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: timeout}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
				if err != nil {
					t.Fatal(err)
				}

				observed, err := applyVLAN(context.Background(), device, desired, true)
				failure := tc.stuck || tc.changed
				if (err != nil) != failure {
					t.Fatalf("observed=%v error=%v", observed, err)
				}
				if tc.stuck && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected deadline error, got %v", err)
				}
				mu.Lock()
				defer mu.Unlock()
				if failure {
					if writes != 0 || saves != 0 {
						t.Fatalf("failed preflight wrote=%d saved=%d", writes, saves)
					}
					return
				}
				if observed == nil || *observed != desired || !present || !savedPresent || priority != desired.Priority || savedPriority != desired.Priority {
					t.Fatalf("observed=%v present=%t native=%d saved=%d", observed, present, priority, savedPriority)
				}
				if tc.noop && writes != 0 {
					t.Fatal("persistence retry mutated REST configuration")
				}
			})
		}
	}
}
