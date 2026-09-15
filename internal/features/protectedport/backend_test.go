package protectedport

import (
	"context"
	"encoding/json"
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
	configuration := "ver 09.0.10k\ninterface ethernet 1/1/12\n port-name PHONE\n protected-port\n disable\ninterface lag 11\n protected-port\nend"
	for _, name := range []string{"ethernet 1/1/12", "lag 11"} {
		got, err := parse(configtest.Parse(t, configuration), name)
		if err != nil || !got.enabled {
			t.Fatalf("%s: state=%+v error=%v", name, got, err)
		}
	}
	got, err := parse(configtest.Parse(t, configuration), "ethernet 1/1/12")
	want := "ver 09.0.10k\n port-name PHONE\n disable\ninterface lag 11\n protected-port\nend"
	if err != nil || strings.Join(got.unowned, "\n") != want {
		t.Fatalf("unowned=%q error=%v", got.unowned, err)
	}
	for _, broken := range []string{
		strings.Replace(configuration, " protected-port", " protected-port extra", 1),
		strings.Replace(configuration, " protected-port", " protected-port\n protected-port", 1),
		strings.Replace(configuration, "end", "interface ethernet 1/1/12\nend", 1),
	} {
		if _, err := parse(configtest.Parse(t, broken), "ethernet 1/1/12"); err == nil {
			t.Fatalf("accepted %q", broken)
		}
	}
}

func TestApply(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		initial, desired, cached, ignore, corrupt, fail bool
	}{
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
					protected := ""
					if native {
						protected = " protected-port\n"
					}
					return "ver 09.0.10kT213\ninterface ethernet 1/1/12\n port-name " + neighbor + "\n" + protected + " disable\ninterface ethernet 1/1/11\n protected-port\nend"
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
					case "/protectedport":
						// Cached metadata deliberately disagrees with native configuration.
						if cached {
							fmt.Fprint(w, `{"icx-openconfig-pp:protectedport":{"interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","protectedport":true}}]}}}`)
						} else {
							fmt.Fprint(w, `{"icx-openconfig-pp:protectedport":[null]}`)
						}
					default:
						t.Errorf("unexpected GET %s", r.URL.Path)
						w.WriteHeader(404)
					}
					return
				}
				writes++
				switch {
				case r.Method == http.MethodPatch && r.URL.Path == "/protectedport":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					encoded, _ := json.Marshal(body)
					want := `{"protectedport":{"interfaces":{"interface":{"config":{"name":"ethernet 1/1/12","protectedport":true},"name":"ethernet 1/1/12"}}}}`
					if string(encoded) != want {
						t.Errorf("unowned or invalid payload: %s", encoded)
					}
					if !tc.ignore && !cached {
						native = true
					}
					cached = true
				case r.Method == http.MethodDelete && r.URL.Path == "/protectedport/interfaces/interface=ethernet 1/1/12":
					if !cached {
						w.WriteHeader(404)
						return
					}
					native, cached = false, false
				default:
					t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				if tc.corrupt {
					neighbor = "CHANGED"
				}
				if fail && r.Method == http.MethodPatch {
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
