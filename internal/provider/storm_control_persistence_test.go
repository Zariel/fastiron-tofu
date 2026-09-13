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

func TestOpenTofuStormControlPersistence(t *testing.T) {
	var mu sync.Mutex
	var running, startup int64
	var cached bool
	falseSave := false
	native := func(rate int64) string {
		if rate == 0 {
			return "ver 09.0.10k\nend"
		}
		return fmt.Sprintf("ver 09.0.10k\ninterface ethernet 1/1/12\n broadcast limit %d kbps\nend", rate)
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
			if r.URL.Path != "/restconf/data/openconfig-interfaces:interfaces/interface=ethernet 1/1/12/config/storm_control_config" {
				t.Errorf("unexpected GET %s", r.URL.Path)
				w.WriteHeader(404)
				return
			}
			fmt.Fprint(w, `{"icx-openconfig-stormcontrol:storm_control_config":[null]}`)
		case http.MethodDelete:
			if !cached {
				w.WriteHeader(404)
				return
			}
			running, cached = 0, false
			w.WriteHeader(204)
		case http.MethodPatch:
			var body struct {
				Policy struct {
					Broadcast struct {
						Rate int64 `json:"limit"`
					} `json:"broadcast"`
				} `json:"storm_control_config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			running, cached = body.Policy.Broadcast.Rate, true
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(405)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(rate int64) {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_interface_storm_control\" \"test\" {\n interface = \"ethernet 1/1/12\"\n unit = \"kbps\"\n broadcast_limit = %d\n}\n", rate))
	}
	choose(111)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")

	assertState := func(rate *int64, pending bool) {
		t.Helper()
		var state struct {
			Values struct {
				Root struct {
					Resources []struct {
						Address string `json:"address"`
						Values  struct {
							Rate    *int64 `json:"broadcast_limit"`
							Pending bool   `json:"persistence_pending"`
						} `json:"values"`
					} `json:"resources"`
				} `json:"root_module"`
			} `json:"values"`
		}
		if err := json.Unmarshal([]byte(run(0, "show", "-json")), &state); err != nil {
			t.Fatal(err)
		}
		for _, resource := range state.Values.Root.Resources {
			if resource.Address != "fastiron_interface_storm_control.test" {
				continue
			}
			values := resource.Values
			if values.Pending != pending || (values.Rate == nil) != (rate == nil) || rate != nil && *values.Rate != *rate {
				t.Fatalf("unexpected storm control state: %+v", values)
			}
			return
		}
		t.Fatal("storm control resource missing from state")
	}
	assertNative := func(wantRunning, wantStartup int64) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if running != wantRunning || startup != wantStartup {
			t.Fatalf("running=%v startup=%v", running, startup)
		}
	}

	choose(222)
	mu.Lock()
	falseSave = true
	mu.Unlock()
	output := run(1, "apply", "-auto-approve", "-no-color")
	if !strings.Contains(output, "startup configuration does not match running configuration") {
		t.Fatalf("missing save verification error: %s", output)
	}
	assertNative(222, 111)
	assertState(new(int64(222)), true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertNative(222, 222)
	assertState(new(int64(222)), false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	falseSave = true
	mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	assertNative(0, 222)
	assertState(nil, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	assertNative(0, 0)
	if strings.Contains(run(0, "state", "list"), "fastiron_interface_storm_control.test") {
		t.Fatal("destroy retained storm control state")
	}
}
