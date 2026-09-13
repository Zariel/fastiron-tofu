package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuDSCPTrustPersistence(t *testing.T) {
	var mu sync.Mutex
	var running, startup bool
	falseSave := false
	native := func(enabled bool) string {
		if !enabled {
			return "ver 09.0.10k\nend"
		}
		return "ver 09.0.10k\ninterface ethernet 1/1/12\n trust dscp\nend"
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
			return native(running)
		case "show configuration":
			return native(startup)
		case "write memory":
			if !falseSave {
				startup = running
			}
			return "Write startup-config done."
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == "/restconf/data/interfaces" {
				fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}}]}}`)
				return
			}
			if r.URL.Path != "/restconf/data/interfaces/interface=ethernet 1/1/12/ethernet/trust-dscp" {
				t.Errorf("unexpected GET %s", r.URL.Path)
				w.WriteHeader(404)
				return
			}
			fmt.Fprint(w, `{"icx-openconfig-if-trust-dscp-aug:trust-dscp":{"config":{"enabled":false}}}`)
		case http.MethodPut:
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
				t.Error("missing enabled value")
				w.WriteHeader(400)
				return
			}
			running = *body.Trust.Config.Enabled
			w.WriteHeader(204)

		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(405)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(enabled bool) {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_interface_dscp_trust\" \"test\" {\n interface = \"ethernet 1/1/12\"\n enabled = %t\n}\n", enabled))
	}
	choose(false)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")

	assertState := func(enabled, pending bool) {
		t.Helper()
		var state struct {
			Values struct {
				Root struct {
					Resources []struct {
						Address string `json:"address"`
						Values  struct {
							Enabled bool `json:"enabled"`
							Pending bool `json:"persistence_pending"`
						} `json:"values"`
					} `json:"resources"`
				} `json:"root_module"`
			} `json:"values"`
		}
		if err := json.Unmarshal([]byte(run(0, "show", "-json")), &state); err != nil {
			t.Fatal(err)
		}
		for _, resource := range state.Values.Root.Resources {
			if resource.Address != "fastiron_interface_dscp_trust.test" {
				continue
			}
			values := resource.Values
			if values.Pending != pending || values.Enabled != enabled {
				t.Fatalf("unexpected DSCP trust state: %+v", values)
			}
			return
		}
		t.Fatal("DSCP trust resource missing from state")
	}
	assertNative := func(wantRunning, wantStartup bool) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if running != wantRunning || startup != wantStartup {
			t.Fatalf("running=%v startup=%v", running, startup)
		}
	}

	choose(true)
	mu.Lock()
	falseSave = true
	mu.Unlock()
	output := run(1, "apply", "-auto-approve", "-no-color")
	if !strings.Contains(output, "startup configuration does not match running configuration") {
		t.Fatalf("missing save verification error: %s", output)
	}
	assertNative(true, false)
	assertState(true, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertNative(true, true)
	assertState(true, false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	falseSave = true
	mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	assertNative(false, true)
	assertState(false, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	assertNative(false, false)
	if strings.Contains(run(0, "state", "list"), "fastiron_interface_dscp_trust.test") {
		t.Fatal("destroy retained DSCP trust state")
	}
}
