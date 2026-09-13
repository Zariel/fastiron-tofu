package ve

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

func TestNameReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name, native, cached, desired                      string
		absentNative, absentCached, stall, ignore, corrupt bool
	}{
		{name: "update", native: "OLD", cached: "OLD", desired: "NEW"},
		{name: "drift", native: "DRIFT", cached: "NEW", desired: "NEW"},
		{name: "native-only name removal", native: "NATIVE"},
		{name: "native no-op", native: "NEW", cached: "OLD", desired: "NEW"},
		{name: "create", absentNative: true, absentCached: true, desired: "NEW"},
		{name: "stale cached interface", absentNative: true, cached: "OLD", desired: "NEW"},
		{name: "stalled cache", native: "DRIFT", cached: "NEW", desired: "NEW", stall: true},
		{name: "false acknowledgement", native: "OLD", cached: "OLD", desired: "NEW", ignore: true},
		{name: "unowned mutation", native: "OLD", cached: "OLD", desired: "NEW", corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			native, cached := tc.native, tc.cached
			exists, cacheExists := !tc.absentNative, !tc.absentCached
			child := exists
			reads, writes, saves := 0, 0, 0
			running := func() string {
				text := "ver 09.0.10kT213\nvlan 53 name TRANSIT by port\ninterface ethernet 1/1/1\n port-name NEIGHBOR\n"
				if exists {
					text += "interface ve 53\n"
					if native != "" {
						text += " port-name " + native + "\n"
					}
					if child {
						text += " disable\n ip address 192.0.2.1/24\n"
					}
				}
				return text + "end"
			}
			startup := running()
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return running()
				case "show configuration":
					return startup
				case "write memory":
					saves++
					startup = running()
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					if r.URL.Path == "/network-instances/network-instance=default-vrf/vlans/vlan=53" {
						fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":53,"config":{"vlan-id":53,"name":"TRANSIT"}}]}`)
						return
					}
					if r.URL.Path != "/interfaces" {
						t.Errorf("unexpected GET %q", r.URL.Path)
						http.NotFound(w, r)
						return
					}
					reads++
					if reads >= 3 && !tc.stall {
						cached, cacheExists = native, exists
					}
					entry := ""
					if cacheExists {
						entry = fmt.Sprintf(`,{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan","description":%q},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`, cached)
					}
					fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"}%s]}}`, entry)
					return
				}
				writes++
				target := "/openconfig-interfaces:interfaces/interface/ve 53/config/description"
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/interfaces":
					var body struct {
						Interface []struct {
							Config struct {
								Description string `json:"description"`
							} `json:"config"`
						} `json:"interface"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Interface) != 1 {
						t.Error("invalid create payload")
						w.WriteHeader(400)
						return
					}
					if cacheExists {
						t.Error("create before cached deletion synchronized")
						w.WriteHeader(409)
						return
					}
					if !tc.ignore {
						native, exists = body.Interface[0].Config.Description, true
					}
					cached, cacheExists = body.Interface[0].Config.Description, true
				case r.Method == http.MethodPut && r.URL.Path == target:
					var body map[string]string
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Error("invalid name payload")
						w.WriteHeader(400)
						return
					}
					desired, ok := body["openconfig-interfaces:description"]
					if !ok {
						t.Error("missing description")
						w.WriteHeader(400)
						return
					}
					// FastIron skips native callbacks when the requested value matches its cache.
					if desired != cached && !tc.ignore {
						native = desired
					}
					cached = desired
				case r.Method == http.MethodDelete && r.URL.Path == target:
					if !tc.ignore {
						native = ""
					}
					cached = ""
				default:
					t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(405)
					return
				}
				if tc.corrupt {
					child = false
				}
				w.WriteHeader(204)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "after_each_write",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}

			observed, err := apply(context.Background(), device, config{ID: 53, VLANID: 53, PortName: tc.desired})
			failure := tc.stall || tc.ignore || tc.corrupt
			if (err != nil) != failure {
				t.Fatalf("observed=%+v error=%v", observed, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if failure {
				if saves != 0 {
					t.Fatal("saved a failed or unverified mutation")
				}
				if tc.stall && writes != 0 {
					t.Fatal("wrote before cache synchronized")
				}
				return
			}
			if observed == nil || observed.PortName != tc.desired || !exists || native != tc.desired || startup != running() || saves != 1 {
				t.Fatalf("observed=%+v exists=%t native=%q saves=%d", observed, exists, native, saves)
			}
			if !tc.absentNative && !strings.Contains(startup, " disable\n ip address 192.0.2.1/24\n") {
				t.Fatal("lost unowned VE children")
			}
			expectedWrites := 1
			if !tc.absentNative && tc.native == tc.desired {
				expectedWrites = 0
			}
			if writes != expectedWrites {
				t.Fatalf("writes=%d, want %d", writes, expectedWrites)
			}
		})
	}
}
