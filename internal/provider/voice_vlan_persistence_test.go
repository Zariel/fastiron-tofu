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

func TestOpenTofuVoiceVLANPersistence(t *testing.T) {
	var mu sync.Mutex
	var running, startup, cached int64
	falseSave := false
	native := func(id int64) string {
		if id == 0 {
			return "ver 09.0.10k\nend"
		}
		return fmt.Sprintf("ver 09.0.10k\ninterface ethernet 1/1/12\n voice-vlan %d\nend", id)
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
			fmt.Fprintf(w, `{"openconfig-if-ethernet:config":{"icx-openconfig-if-ethernet-aug:ip-voice-vlan":%d}}`, cached)
		case http.MethodDelete:
			if cached == 0 {
				w.WriteHeader(404)
				return
			}
			running, cached = 0, 0
			w.WriteHeader(204)
		case http.MethodPatch:
			var body struct {
				Config struct {
					ID int64 `json:"ip-voice-vlan"`
				} `json:"openconfig-if-ethernet:config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			running, cached = body.Config.ID, body.Config.ID
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(405)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(id int64) {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_interface_voice_vlan\" \"test\" {\n interface = \"ethernet 1/1/12\"\n vlan_id = %d\n}\n", id))
	}
	choose(3053)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")

	assertState := func(id int64, pending bool) {
		t.Helper()
		var state struct {
			Values struct {
				Root struct {
					Resources []struct {
						Address string `json:"address"`
						Values  struct {
							VLANID  *int64 `json:"vlan_id"`
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
			if resource.Address != "fastiron_interface_voice_vlan.test" {
				continue
			}
			values := resource.Values
			if values.Pending != pending || (id == 0 && values.VLANID != nil) || (id != 0 && (values.VLANID == nil || *values.VLANID != id)) {
				t.Fatalf("unexpected voice VLAN state: %+v", values)
			}
			return
		}
		t.Fatal("voice VLAN resource missing from state")
	}
	assertNative := func(wantRunning, wantStartup int64) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if running != wantRunning || startup != wantStartup {
			t.Fatalf("running=%d startup=%d", running, startup)
		}
	}

	choose(3054)
	mu.Lock()
	falseSave = true
	mu.Unlock()
	output := run(1, "apply", "-auto-approve", "-no-color")
	if !strings.Contains(output, "startup configuration does not match running configuration") {
		t.Fatalf("missing save verification error: %s", output)
	}
	assertNative(3054, 3053)
	assertState(3054, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertNative(3054, 3054)
	assertState(3054, false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	falseSave = true
	mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	assertNative(0, 3054)
	assertState(0, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	assertNative(0, 0)
	if strings.Contains(run(0, "state", "list"), "fastiron_interface_voice_vlan.test") {
		t.Fatal("destroy retained voice VLAN state")
	}
}
