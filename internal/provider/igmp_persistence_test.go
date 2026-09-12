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

func TestOpenTofuIGMPPersistence(t *testing.T) {
	var mu sync.Mutex
	running, startup, cached := int64(2), int64(2), int64(0)
	falseSave := false
	native := func(version int64) string {
		if version == 2 {
			return "ver 09.0.10k\nend"
		}
		return "ver 09.0.10k\nip multicast version 3\nend"
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
			fmt.Fprintf(w, `{"icx-igmp-mld-snooping:global":{"igmp":{"config":{"version":%d}}}}`, cached)
		case http.MethodDelete:
			if cached == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			running, cached = 2, 0
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPatch:
			var body struct {
				Config struct {
					Version int64 `json:"version"`
				} `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			running, cached = body.Config.Version, body.Config.Version
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_igmp_snooping" "test" {}`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")

	assertState := func(version int64, pending bool) {
		t.Helper()
		type resource struct {
			Address string `json:"address"`
			Values  struct {
				Version int64 `json:"version"`
				Pending bool  `json:"persistence_pending"`
			} `json:"values"`
		}
		var state struct {
			Values struct {
				Root struct {
					Resources []resource `json:"resources"`
				} `json:"root_module"`
			} `json:"values"`
		}
		if err := json.Unmarshal([]byte(run(0, "show", "-json")), &state); err != nil {
			t.Fatal(err)
		}
		for _, resource := range state.Values.Root.Resources {
			if resource.Address != "fastiron_igmp_snooping.test" {
				continue
			}
			if resource.Values.Version != version || resource.Values.Pending != pending {
				t.Fatalf("unexpected resource state: %+v", resource.Values)
			}
			return
		}
		t.Fatal("global IGMP resource missing from state")
	}
	assertNative := func(wantRunning, wantStartup int64) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if running != wantRunning || startup != wantStartup {
			t.Fatalf("running=%d startup=%d", running, startup)
		}
	}

	write("main.tf", base+`resource "fastiron_igmp_snooping" "test" { version = 3 }`)
	mu.Lock()
	falseSave = true
	mu.Unlock()
	output := run(1, "apply", "-auto-approve", "-no-color")
	if !strings.Contains(output, "startup configuration does not match running configuration") {
		t.Fatalf("missing save verification error: %s", output)
	}
	assertNative(3, 2)
	assertState(3, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertNative(3, 3)
	assertState(3, false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	falseSave = true
	mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	assertNative(2, 3)
	assertState(2, true)

	mu.Lock()
	falseSave = false
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	assertNative(2, 2)
	if strings.Contains(run(0, "state", "list"), "fastiron_igmp_snooping.test") {
		t.Fatal("destroy retained global IGMP state")
	}
}
