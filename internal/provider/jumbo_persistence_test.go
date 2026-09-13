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

func TestOpenTofuJumboPersistence(t *testing.T) {
	var mu sync.Mutex
	var running, startup, active bool
	mutations := 0
	falseSave := false
	native := func(enabled bool) string {
		if !enabled {
			return "ver 09.0.10k\nend"
		}
		return "ver 09.0.10k\njumbo\nend"
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
			mutations++
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
			if r.URL.Path != "/restconf/data/jumbo" {
				t.Errorf("unexpected GET %s", r.URL.Path)
				w.WriteHeader(404)
				return
			}
			fmt.Fprintf(w, `{"icx-openconfig-jumbo:jumbo":{"config":{"enabled":%t},"operation-state":{"enabled":%t}}}`, running, active)

		case http.MethodPut:
			mutations++
			var body struct {
				Trust struct {
					Config struct {
						Enabled *bool `json:"enabled"`
					} `json:"config"`
				} `json:"icx-openconfig-jumbo:jumbo"`
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
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_jumbo\" \"test\" {\n enabled = %t\n}\n", enabled))
	}
	choose(false)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")

	assertState := func(enabled, pending, wantActive, reload bool) {
		t.Helper()
		var state struct {
			Values struct {
				Root struct {
					Resources []struct {
						Address string `json:"address"`
						Values  struct {
							Enabled bool `json:"enabled"`
							Pending bool `json:"persistence_pending"`
							Active  bool `json:"active_enabled"`
							Reload  bool `json:"reload_required"`
						} `json:"values"`
					} `json:"resources"`
				} `json:"root_module"`
			} `json:"values"`
		}
		if err := json.Unmarshal([]byte(run(0, "show", "-json")), &state); err != nil {
			t.Fatal(err)
		}
		for _, resource := range state.Values.Root.Resources {
			if resource.Address != "fastiron_jumbo.test" {
				continue
			}
			values := resource.Values
			if values.Pending != pending || values.Enabled != enabled || values.Active != wantActive || values.Reload != reload {
				t.Fatalf("unexpected jumbo state: %+v", values)
			}
			return
		}
		t.Fatal("jumbo resource missing from state")
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
	assertState(true, true, false, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertNative(true, true)
	assertState(true, false, false, true)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	beforeImport := mutations
	mu.Unlock()
	run(0, "state", "rm", "fastiron_jumbo.test")
	if output := run(1, "import", "-no-color", "fastiron_jumbo.test", "invalid"); !strings.Contains(output, "Invalid jumbo identity") {
		t.Fatalf("unexpected import error: %s", output)
	}
	run(0, "import", "-no-color", "fastiron_jumbo.test", "global")
	assertState(true, false, false, true)
	mu.Lock()
	if mutations != beforeImport {
		t.Error("import changed switch configuration")
	}
	running = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertNative(true, true)
	run(0, "apply", "-auto-approve", "-no-color", "-replace=fastiron_jumbo.test")
	assertNative(true, true)
	// An external reload activates the saved configuration without a provider write.
	mu.Lock()
	active = startup
	beforeReloadRefresh := mutations
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	assertState(true, false, true, false)
	mu.Lock()
	if mutations != beforeReloadRefresh {
		t.Error("refresh changed switch configuration")
	}
	mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	falseSave = true
	mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	assertNative(false, true)
	assertState(false, true, true, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	assertNative(false, false)
	if strings.Contains(run(0, "state", "list"), "fastiron_jumbo.test") {
		t.Fatal("destroy retained jumbo state")
	}
}
