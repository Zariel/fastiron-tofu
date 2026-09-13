package jumbo

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

func TestApply(t *testing.T) {
	for _, tc := range []struct {
		name                                                                  string
		initial, cached, desired, stall, ignore, corrupt, failWrite, failSave bool
	}{
		{name: "enable", desired: true},
		{name: "disable", initial: true, cached: true},
		{name: "enable after CLI drift", cached: true, desired: true},
		{name: "disable native-only mode", initial: true},
		{name: "native no-op with stale cache", initial: true, desired: true},
		{name: "cache never synchronizes", cached: true, desired: true, stall: true},
		{name: "false acknowledgement", desired: true, ignore: true},
		{name: "unowned change", desired: true, corrupt: true},
		{name: "ambiguous failure", desired: true, failWrite: true},
		{name: "save acknowledgement without persistence", desired: true, failSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			native, cached, neighbor := tc.initial, tc.cached, "EDGE"
			reads, writes, saves := 0, 0, 0
			failWrite, failSave := tc.failWrite, tc.failSave
			configuration := func(enabled bool, name string) string {
				flag := ""
				if enabled {
					flag = "jumbo\n"
				}
				return "ver 09.0.10kT213\n" + flag + "interface ethernet 1/1/12\n port-name " + name + "\nend"
			}
			startup := configuration(native, neighbor)
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return configuration(native, neighbor)
				case "show configuration":
					return startup
				case "write memory":
					saves++
					if failSave {
						failSave = false
					} else {
						startup = configuration(native, neighbor)
					}
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/jumbo", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					reads++
					// Model independently observed cache catch-up after CLI changes.
					if reads >= 3 && !tc.stall {
						cached = native
					}
					fmt.Fprintf(w, `{"icx-openconfig-jumbo:jumbo":{"config":{"enabled":%t},"operation-state":{"enabled":false}}}`, cached)
					return
				}
				if r.Method != http.MethodPut {
					t.Errorf("unexpected mutation %s", r.Method)
					w.WriteHeader(405)
					return
				}
				writes++
				var payload struct {
					Jumbo struct {
						Config struct {
							Enabled *bool `json:"enabled"`
						} `json:"config"`
					} `json:"icx-openconfig-jumbo:jumbo"`
				}
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Jumbo.Config.Enabled == nil {
					t.Error("missing enabled value")
					w.WriteHeader(400)
					return
				}
				desired := *payload.Jumbo.Config.Enabled
				// FastIron may skip a callback for an unchanged cached value and reject
				// an attempt to reapply the current native mode.
				if cached != desired {
					if native == desired {
						w.WriteHeader(500)
						return
					}
					if !tc.ignore {
						native = desired
					}
				}
				cached = desired
				if tc.corrupt {
					neighbor = "CHANGED"
				}
				if failWrite {
					failWrite = false
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(204)
			})
			device, err := fastiron.New(fastiron.Config{Host: "switch", Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := apply(context.Background(), device, tc.desired)
			if tc.stall || tc.ignore || tc.corrupt {
				want := "did not converge"
				if tc.stall {
					want = "did not synchronize"
				}
				if tc.corrupt {
					want = "unrelated configuration"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected %s; got %v", want, err)
				}
				mu.Lock()
				defer mu.Unlock()
				if saves != 0 || tc.stall && writes != 0 {
					t.Fatalf("unsafe persistence or mutation: writes=%d saves=%d", writes, saves)
				}
				if observed == nil || observed.enabled != native {
					t.Fatal("lost observed state on failure")
				}
				return
			}
			if tc.failWrite || tc.failSave {
				if err == nil || observed == nil || observed.enabled != tc.desired {
					t.Fatalf("lost partial state: %v %v", observed, err)
				}
				observed, err = apply(context.Background(), device, tc.desired)
			}
			if err != nil || observed == nil || observed.enabled != tc.desired {
				t.Fatalf("observed=%v error=%v", observed, err)
			}
			mu.Lock()
			previousWrites := writes
			mu.Unlock()
			if _, err := apply(context.Background(), device, tc.desired); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != previousWrites || native != tc.desired || startup != configuration(tc.desired, "EDGE") {
				t.Fatal("state did not persist or repeated apply changed native configuration")
			}
		})
	}
}
