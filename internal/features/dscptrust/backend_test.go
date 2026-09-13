package dscptrust

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
		name                                                 string
		initial, desired, cached, ignore, corrupt, fail, sfc bool
	}{
		{name: "flow control enable", desired: true, sfc: true},
		{name: "flow control removal alignment", initial: true, sfc: true},
		{name: "flow control disabled no-op", sfc: true},
		{name: "enable", desired: true},
		{name: "CLI drift", desired: true, cached: true},
		{name: "native-only deletion", initial: true},
		{name: "delete", initial: true, cached: true},
		{name: "no change", initial: true, desired: true},
		{name: "false acknowledgement", desired: true, ignore: true},
		{name: "unrelated mutation", desired: true, corrupt: true},
		{name: "ambiguous failure", desired: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			native, cached, neighbor, writes, fail := tc.initial, tc.cached, "PHONE", 0, tc.fail
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					trust := ""
					if native {
						trust = " trust dscp\n"
					}
					global := ""
					if tc.sfc {
						global = "symmetrical-flow-control enable\n"
					}
					return "ver 09.0.10kT213\n" + global + "interface ethernet 1/1/12\n port-name " + neighbor + "\n" + trust + " disable\ninterface ethernet 1/1/11\n trust dscp\nend"
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					switch r.URL.Path {
					case "/interfaces":
						fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}}]}}`)
					case "/interfaces/interface=ethernet 1/1/12/ethernet/trust-dscp":
						fmt.Fprintf(w, `{"icx-openconfig-if-trust-dscp-aug:trust-dscp":{"config":{"enabled":%t}}}`, cached)

					default:
						t.Errorf("unexpected GET %s", r.URL.Path)
						w.WriteHeader(404)
					}
					return
				}
				writes++
				if r.Method != http.MethodPut || r.URL.Path != "/interfaces/interface=ethernet 1/1/12/ethernet/trust-dscp" {
					t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				var body struct {
					Trust struct {
						Config struct {
							Enabled *bool `json:"enabled"`
						} `json:"config"`
					} `json:"icx-openconfig-if-trust-dscp-aug:trust-dscp"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Trust.Config.Enabled == nil {
					t.Error("missing enable value")
					w.WriteHeader(400)
					return
				}
				value := *body.Trust.Config.Enabled
				if !tc.ignore && cached != value {
					native = value
				}
				cached = value

				if tc.corrupt {
					neighbor = "CHANGED"
				}
				if fail && value == tc.desired {
					fail = false
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(204)
			})
			device, err := fastiron.New(fastiron.Config{Host: "switch", Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := apply(context.Background(), device, "ethernet 1/1/12", tc.desired)
			if tc.sfc && (tc.initial || tc.desired) {
				mu.Lock()
				defer mu.Unlock()
				if err == nil || !strings.Contains(err.Error(), "global symmetrical flow control") || writes != 0 || native != tc.initial {
					t.Fatalf("unsafe flow-control interaction: writes=%d native=%v error=%v", writes, native, err)
				}
				return
			}
			if tc.ignore || tc.corrupt {
				want := "did not converge"
				if tc.corrupt {
					want = "unrelated configuration"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected %s; observed=%v error=%v", want, observed, err)
				}
				return
			}
			if tc.fail {
				if err == nil || observed == nil || !*observed {
					t.Fatalf("lost partial state: %v %v", observed, err)
				}
				observed, err = apply(context.Background(), device, "ethernet 1/1/12", tc.desired)
			}
			if err != nil || observed == nil || *observed != tc.desired {
				t.Fatalf("observed=%v error=%v", observed, err)
			}
			mu.Lock()
			before := writes
			mu.Unlock()
			if _, err := apply(context.Background(), device, "ethernet 1/1/12", tc.desired); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != before || native != tc.desired || neighbor != "PHONE" {
				t.Fatalf("writes=%d before=%d native=%v neighbor=%q", writes, before, native, neighbor)
			}
		})
	}
}
