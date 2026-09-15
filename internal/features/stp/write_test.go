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

func TestInterfaceWrite(t *testing.T) {
	desired := interfaceConfig{AdminEdge: true, BPDUGuard: true, RootGuard: true}
	for _, tc := range []struct {
		name, target                                          string
		stale, partial, delayed, stuck, fail, corrupt, ignore bool
	}{
		{name: "normal"},
		{name: "LAG", target: "lag 11"},
		{name: "LAG cached desired", target: "lag 11", stale: true},
		{name: "LAG stale BPDU", target: "lag 11", partial: true},
		{name: "cached desired", stale: true},
		{name: "lagging cache", stale: true, delayed: true},
		{name: "cache timeout", stale: true, stuck: true},
		{name: "partial failure", fail: true},
		{name: "priming failure", stale: true, fail: true},
		{name: "unrelated mutation", corrupt: true},
		{name: "ignored mutation", ignore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.target
			if target == "" {
				target = "ethernet 1/1/12"
			}
			var mu sync.Mutex
			current, saved, cached := interfaceConfig{}, interfaceConfig{}, interfaceConfig{}
			if tc.stale {
				cached = desired
			}
			if tc.partial {
				cached.BPDUGuard = true
			}
			writes, saves, lag := 0, 0, 0
			var pending *interfaceConfig
			native := func(flags interfaceConfig) string {
				neighbor := "neighbor"
				if tc.corrupt && writes > 0 {
					neighbor = "changed"
				}
				parent := ""
				if target == "lag 11" {
					parent = "lag test static id 11\n ports ethe 1/1/9 to 1/1/10\n"
				}
				lines := "ver 09.0.10k\n" + parent + "interface ethernet 1/1/11\n port-name " + neighbor + "\n stp-bpdu-guard\ninterface " + target + "\n port-name phone\n"
				if flags.AdminEdge {
					lines += " spanning-tree 802-1w admin-edge-port\n"
				}
				if flags.BPDUGuard {
					lines += " stp-bpdu-guard\n"
				}
				if flags.RootGuard {
					lines += " spanning-tree root-protect\n"
				}
				return lines + "end"
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
					return native(current)
				case "show configuration":
					return native(saved)
				case "write memory":
					saved = current
					saves++
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/interfaces", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":%q,"config":{"name":%q}}]}}`, target, target)
			})
			server.HandleFunc("/stp/interfaces", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					if pending != nil {
						lag--
						if lag == 0 {
							cached = *pending
							pending = nil
						}
					}
					edge, guard := "EDGE_DISABLE", "NONE"
					if cached.AdminEdge {
						edge = "EDGE_ENABLE"
					}
					if cached.RootGuard {
						guard = "ROOT"
					}
					entries := []any{map[string]any{"name": target, "config": map[string]any{"name": target, "edge-port": edge, "guard": guard, "bpdu-guard": cached.BPDUGuard}}}
					// A new default-only cache identity is not a native neighbor mutation.
					if writes > 0 {
						entries = append(entries, map[string]any{"name": "ethernet 1/1/14", "config": map[string]any{"name": "ethernet 1/1/14"}})
					}
					json.NewEncoder(w).Encode(map[string]any{"openconfig-spanning-tree:interfaces": map[string]any{"interface": entries}})
					return
				}
				if r.Method != http.MethodPatch {
					t.Errorf("unexpected mutation %s", r.Method)
					w.WriteHeader(405)
					return
				}
				var body struct {
					Interfaces struct {
						Interface []struct {
							Name   string `json:"name"`
							Config struct {
								Edge  string `json:"edge-port"`
								Guard string `json:"guard"`
								BPDU  bool   `json:"bpdu-guard"`
							} `json:"config"`
						} `json:"interface"`
					} `json:"interfaces"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if len(body.Interfaces.Interface) != 1 || body.Interfaces.Interface[0].Name != target {
					t.Error("wrong target")
					w.WriteHeader(400)
					return
				}
				c := body.Interfaces.Interface[0].Config
				value := interfaceConfig{AdminEdge: strings.HasSuffix(c.Edge, "EDGE_ENABLE"), RootGuard: c.Guard == "ROOT", BPDUGuard: c.BPDU}
				writes++
				if !tc.ignore {
					if value.AdminEdge != cached.AdminEdge {
						current.AdminEdge = value.AdminEdge
					}
					if value.BPDUGuard != cached.BPDUGuard {
						current.BPDUGuard = value.BPDUGuard
					}
					if value.RootGuard != cached.RootGuard {
						current.RootGuard = value.RootGuard
					}
				}
				if tc.delayed && value == (interfaceConfig{}) {
					pending = &value
					lag = 2
				} else if !tc.stuck {
					cached = value
				}
				if tc.fail {
					http.Error(w, "partial mutation", 500)
					return
				}
				w.WriteHeader(204)
			})
			timeout := 5 * time.Second
			if tc.stuck {
				timeout = 500 * time.Millisecond
			}
			device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: timeout}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			observed, err := applyInterface(context.Background(), device, target, desired)
			failure := tc.fail || tc.corrupt || tc.ignore || tc.stuck
			if tc.stuck && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected cache deadline, got %v", err)
			}
			if (err != nil) != failure || observed == nil {
				t.Fatalf("observed=%v error=%v", observed, err)
			}
			mu.Lock()
			if *observed != current {
				t.Errorf("reported %v; native %v", observed, current)
			}
			if failure && saves != 0 {
				t.Errorf("failed operation saved %d times", saves)
			}
			if !failure && (current != desired || saved != desired) {
				t.Errorf("native=%v saved=%v; want %v", current, saved, desired)
			}
			firstWrites := writes
			if (tc.fail || tc.stuck) && firstWrites != 1 {
				t.Errorf("continued after failed mutation: writes=%d", firstWrites)
			}
			mu.Unlock()
			if tc.corrupt || tc.ignore || tc.stuck || (tc.stale && tc.fail) {
				return
			}

			if _, err := applyInterface(context.Background(), device, target, desired); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != firstWrites || saved != desired {
				t.Fatalf("retry wrote again or failed to save: writes=%d before=%d saved=%v", writes, firstWrites, saved)
			}
		})
	}
}
