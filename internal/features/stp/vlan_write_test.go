package stp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestVLANWriteOwnership(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "testdata", "stp", "classic-default.conf"))
	if err != nil {
		t.Fatal(err)
	}
	target := "vlan 3053 name TOFU-TEST by port\n tagged ethe 1/1/12 \n spanning-tree\n"
	if !strings.Contains(string(raw), target) {
		t.Fatal("capture is missing the target policy")
	}
	for _, present := range []bool{true, false} {
		for _, tc := range []struct{ name, old, new string }{
			{name: "preserved"},
			{name: "VLAN name", old: "vlan 3053 name TOFU-TEST", new: "vlan 3053 name CHANGED"},
			{name: "membership", old: " tagged ethe 1/1/12 ", new: " tagged ethe 1/1/10 "},
			{name: "neighbor interface", old: " port-name wifi", new: " port-name CHANGED"},
			{name: "VLAN removed", old: "vlan 3053 name TOFU-TEST by port\n", new: ""},
		} {
			t.Run(fmt.Sprintf("present=%t/%s", present, tc.name), func(t *testing.T) {
				var mu sync.Mutex
				running, saved := string(raw), string(raw)
				enabled, priority, saves := true, int64(32768), 0
				server := testswitch.New(t, func(command string) string {
					mu.Lock()
					defer mu.Unlock()
					switch command {
					case "skip-page-display":
						return ""
					case "show version":
						return "SW: Version 09.0.10kT213"
					case "show running-config":
						return running
					case "show configuration":
						return saved
					case "write memory":
						saved = running
						saves++
						return "Write startup-config done."
					default:
						t.Errorf("unexpected command %q", command)
						return "% Invalid input"
					}
				})
				server.HandleFunc("/network-instances/network-instance=default-vrf/vlans/vlan=3053", func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":3053,"config":{"vlan-id":3053,"name":"TOFU-TEST"}}]}`)
				})
				server.HandleFunc("/stp", func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					entries := []any{}
					if enabled {
						entries = append(entries, map[string]any{"vlan-id": 3053, "config": map[string]any{"vlan-id": 3053, "pvst-priority": priority}})
					}
					json.NewEncoder(w).Encode(map[string]any{"openconfig-spanning-tree:stp": map[string]any{"rapid-pvst": map[string]any{}, "icx-openconfig-spanning-tree-aug:pvst": map[string]any{"vlan": entries}}})
				})
				mutate := func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					replacement := "vlan 3053 name TOFU-TEST by port\n tagged ethe 1/1/12 \n"
					if present {
						var body struct {
							PVST struct {
								VLAN []struct {
									Config struct {
										Priority int64 `json:"pvst-priority"`
									} `json:"config"`
								} `json:"vlan"`
							} `json:"pvst"`
						}
						if r.Method != http.MethodPatch || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.PVST.VLAN) != 1 {
							t.Error("invalid PATCH")
							w.WriteHeader(400)
							return
						}
						priority = body.PVST.VLAN[0].Config.Priority
						replacement += fmt.Sprintf(" spanning-tree priority %d\n", priority)
					} else {
						if r.Method != http.MethodDelete {
							t.Error("expected DELETE")
							w.WriteHeader(400)
							return
						}
						enabled = false
					}
					running = strings.Replace(running, target, replacement, 1)
					if tc.old != "" {
						running = strings.Replace(running, tc.old, tc.new, 1)
					}
					w.WriteHeader(204)
				}
				server.HandleFunc("/stp/icx-openconfig-spanning-tree-aug:pvst", mutate)
				server.HandleFunc("/stp/icx-openconfig-spanning-tree-aug:pvst/vlan=3053", mutate)
				device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: 500 * time.Millisecond}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
				if err != nil {
					t.Fatal(err)
				}

				desired := vlan{VLANID: 3053, Mode: "stp", Priority: 23456}
				observed, err := applyVLAN(context.Background(), device, desired, present)
				failure := tc.old != ""
				if (err != nil) != failure {
					t.Fatalf("observed=%v error=%v", observed, err)
				}
				mu.Lock()
				defer mu.Unlock()
				if failure {
					if saves != 0 || saved != string(raw) {
						t.Fatal("saved unrelated mutation")
					}
					return
				}
				if saved != running || saves != 1 {
					t.Fatal("did not persist verified configuration")
				}
				if present && (observed == nil || *observed != desired) {
					t.Fatalf("observed=%v; want %v", observed, desired)
				}
				if !present && observed != nil {
					t.Fatalf("deleted policy remains: %v", observed)
				}
			})
		}
	}
}
