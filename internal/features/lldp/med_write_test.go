package lldp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestMEDWrites(t *testing.T) {
	for _, scenario := range []string{"aliases", "cached desired", "uncached native", "delete", "partial delete", "partial create", "echo", "unowned", "zero priority"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			target := "lldp med network-policy application voice untagged dscp 24 ports ethe 1/1/11\n"
			unowned := "lldp med network-policy application voice tagged vlan 3053 priority 3 dscp 46 ports ethe 1/1/10\nlldp med network-policy application video-signaling untagged dscp 0 ports ethe 1/1/11\n"
			originalUnowned := unowned
			cachedU, cachedT, cachedP := true, true, false
			if scenario == "cached desired" {
				target = ""
				cachedU = false
				cachedT = false
				cachedP = true
			}
			if scenario == "uncached native" {
				cachedU = false
				cachedT = false
			}
			native := func() string { return "ver 09.0.10kT213\n" + target + unowned + "end" }
			saved := native()
			originalSaved := saved
			writes, saves := 0, 0
			failed := false
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return native()
				case "show configuration":
					return saved
				case "write memory":
					saves++
					saved = native()
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/interfaces":
					fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[]}}`)
				case r.Method == http.MethodGet && r.URL.Path == "/lldp/interfaces":
					fmt.Fprint(w, `{"openconfig-lldp:interfaces":{"interface":[{"name":"ethernet 1/1/10","config":{"name":"ethernet 1/1/10","enabled":true}},{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11","enabled":true}}]}}`)
				case r.Method == http.MethodGet && r.URL.Path == "/lldp/med":
					groups := []string{`{"application":"video-signaling","traffic":"untagged","untagged":[{"dscp":0,"ports":["ethernet 1/1/11"]}]}`}
					taggedPorts := `"ethernet 1/1/10"`
					if cachedT {
						taggedPorts += `,"ethernet 1/1/11"`
					}
					groups = append(groups, fmt.Sprintf(`{"application":"voice","traffic":"tagged","tagged":[{"vlan":3053,"priority":3,"dscp":46,"ports":[%s]}]}`, taggedPorts))
					if cachedU {
						groups = append(groups, `{"application":"voice","traffic":"untagged","untagged":[{"dscp":24,"ports":["ethernet 1/1/11"]}]}`)
					}
					if cachedP {
						groups = append(groups, `{"application":"voice","traffic":"priority-tagged","priority-tagged":[{"priority":5,"dscp":40,"ports":["ethernet 1/1/11"]}]}`)
					}
					fmt.Fprintf(w, `{"icx-openconfig-lldp-aug:med":{"network-policy":[%s]}}`, strings.Join(groups, ","))
				case r.Method == http.MethodDelete:
					switch r.URL.Path {
					case "/lldp/med/network-policy=voice,untagged/untagged=24/ports=ethernet 1/1/11":
						cachedU = false
					case "/lldp/med/network-policy=voice,tagged/tagged=3053,3,46/ports=ethernet 1/1/11":
						cachedT = false
					case "/lldp/med/network-policy=voice,priority-tagged/priority-tagged=5,40/ports=ethernet 1/1/11":
						cachedP = false
					default:
						t.Errorf("delete escaped ownership: %s", r.URL.Path)
						w.WriteHeader(400)
						return
					}
					writes++
					target = ""
					if scenario == "unowned" {
						unowned = ""
					}
					if scenario == "partial delete" && !failed {
						failed = true
						http.Error(w, "partial deletion", 500)
						return
					}
					w.WriteHeader(204)
				case r.Method == http.MethodPatch && r.URL.Path == "/lldp/med":
					var request struct {
						MED struct {
							Policies []struct {
								Application string `json:"application"`
								Traffic     string `json:"traffic"`
								Untagged    []struct {
									DSCP  int64    `json:"dscp"`
									Ports []string `json:"ports"`
								} `json:"untagged"`
								PriorityTagged []struct {
									Priority int64    `json:"priority"`
									DSCP     int64    `json:"dscp"`
									Ports    []string `json:"ports"`
								} `json:"priority-tagged"`
							} `json:"network-policy"`
						} `json:"icx-openconfig-lldp-aug:med"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.MED.Policies) != 1 {
						t.Errorf("bad mutation body: %v", err)
						w.WriteHeader(400)
						return
					}
					p := request.MED.Policies[0]
					if p.Application != "voice" {
						t.Errorf("changed another application: %s", p.Application)
						w.WriteHeader(400)
						return
					}
					writes++
					switch p.Traffic {
					case "untagged":
						if len(p.Untagged) != 1 || p.Untagged[0].DSCP != 24 || strings.Join(p.Untagged[0].Ports, ",") != "ethernet 1/1/11" {
							t.Error("invalid synchronization payload")
							w.WriteHeader(400)
							return
						}
						if !cachedU {
							target = "lldp med network-policy application voice untagged dscp 24 ports ethe 1/1/11\n"
						}
						cachedU = true
					case "priority-tagged":
						wantPriority := int64(5)
						if scenario == "zero priority" {
							wantPriority = 0
						}
						if len(p.PriorityTagged) != 1 || p.PriorityTagged[0].Priority != wantPriority || p.PriorityTagged[0].DSCP != 40 || strings.Join(p.PriorityTagged[0].Ports, ",") != "ethernet 1/1/11" {
							t.Error("invalid desired payload")
							w.WriteHeader(400)
							return
						}
						if !cachedP && scenario != "echo" {
							target = "lldp med network-policy application voice priority-tagged priority 5 dscp 40 ports ethe 1/1/11\n"
						}
						cachedP = true
						if scenario == "zero priority" {
							target = "lldp med network-policy application voice untagged dscp 40 ports ethe 1/1/11\n"
						}
					default:
						t.Errorf("unexpected traffic %s", p.Traffic)
						w.WriteHeader(400)
						return
					}
					if scenario == "partial create" && !failed {
						failed = true
						http.Error(w, "partial creation", 500)
						return
					}
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			})
			device, err := fastiron.New(fastiron.Config{Host: "switch", Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			desired := &config.MEDPolicy{Traffic: "priority-tagged", Priority: 5, DSCP: 40}
			if scenario == "zero priority" {
				desired.Priority = 0
			}
			if scenario == "delete" {
				desired = nil
			}
			observed, err := applyMED(context.Background(), device, "ethernet 1/1/11", "voice", desired)
			mu.Lock()
			if strings.HasPrefix(scenario, "partial") || scenario == "echo" || scenario == "unowned" || scenario == "zero priority" {
				if err == nil || saves != 0 || saved != originalSaved {
					t.Errorf("failed mutation saved: result=%+v error=%v saves=%d", observed, err, saves)
				}
				if scenario == "unowned" && !strings.Contains(err.Error(), "unrelated") {
					t.Errorf("missing preservation diagnostic: %v", err)
				}
				if scenario == "partial delete" && writes != 1 {
					t.Errorf("continued after partial deletion: writes=%d", writes)
				}
			} else if err != nil || !observed.verified || saves != 1 || saved != native() || unowned != originalUnowned || (desired == nil && target != "") || (desired != nil && (observed.policy == nil || *observed.policy != *desired)) {
				t.Errorf("did not converge and persist: result=%+v error=%v saves=%d", observed, err, saves)
			}
			beforeWrites := writes
			mu.Unlock()
			if !strings.HasPrefix(scenario, "partial") {
				return
			}

			observed, err = applyMED(context.Background(), device, "ethernet 1/1/11", "voice", desired)
			mu.Lock()
			defer mu.Unlock()
			if err != nil || !observed.verified || observed.policy == nil || *observed.policy != *desired || saves != 1 || saved != native() || unowned != originalUnowned {
				t.Fatalf("retry failed: result=%+v error=%v saves=%d", observed, err, saves)
			}
			if scenario == "partial create" && writes != beforeWrites {
				t.Fatal("retry repeated completed mutation")
			}
		})
	}
}
