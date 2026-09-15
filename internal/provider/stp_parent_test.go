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

func TestOpenTofuSTPParent(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			var mu sync.Mutex
			parent, guard, failSave := true, true, false
			detached := false
			patches := 0
			native := func() string {
				if !parent {
					if detached {
						return "ver 09.0.10k\ninterface ethernet 1/1/9\n stp-bpdu-guard\ninterface ethernet 1/1/10\n stp-bpdu-guard\nend"
					}
					return "ver 09.0.10k\nend"
				}
				raw := "ver 09.0.10k\nlag test static id 11\n ports ethe 1/1/9 to 1/1/10\ninterface lag 11\n"
				if guard {
					raw += " stp-bpdu-guard\n"
				}
				return raw + "end"
			}
			saved := native()
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
					if failSave {
						return "% Error saving configuration"
					}
					saved = native()
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("GET /restconf/data/interfaces", func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				entries := []any{map[string]any{"name": "ethernet 1/1/11", "config": map[string]any{"name": "ethernet 1/1/11"}}}
				// The deleted aggregate can remain in RESTCONF after flags move to detached ports.
				entries = append(entries, map[string]any{"name": "lag 11", "config": map[string]any{"name": "lag 11"}})
				json.NewEncoder(w).Encode(map[string]any{"openconfig-interfaces:interfaces": map[string]any{"interface": entries}})
			})
			server.HandleFunc("/restconf/data/stp/interfaces", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					fmt.Fprintf(w, `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"lag 11","config":{"name":"lag 11","bpdu-guard":%t}}]}}`, guard)
					return
				}
				if r.Method != http.MethodPatch || !parent {
					t.Errorf("unexpected %s with parent=%t", r.Method, parent)
					http.Error(w, "invalid mutation", 400)
					return
				}
				var body struct {
					Interfaces struct {
						Interface []struct {
							Config struct {
								Guard bool `json:"bpdu-guard"`
							} `json:"config"`
						} `json:"interface"`
					} `json:"interfaces"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Interfaces.Interface) != 1 {
					t.Errorf("invalid body: %v", err)
					http.Error(w, "invalid body", 400)
					return
				}
				guard = body.Interfaces.Interface[0].Config.Guard
				patches++
				w.WriteHeader(http.StatusNoContent)
			})
			s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
			write, run, base := tofuFixture(t, s)
			write("main.tf", base+`resource "fastiron_spanning_tree_interface" "test" {
 interface = "lag 11"
 bpdu_guard = true
}
`)
			run(0, "init", "-no-color")
			run(0, "import", "-no-color", "fastiron_spanning_tree_interface.test", "lag 11")
			write("main.tf", base)
			if pending {
				mu.Lock()
				failSave = true
				mu.Unlock()
				run(1, "apply", "-auto-approve", "-no-color")
				if state := run(0, "state", "show", "-no-color", "fastiron_spanning_tree_interface.test"); !strings.Contains(state, "persistence_pending = true") {
					t.Fatalf("failed save is not retained: %s", state)
				}
			}
			mu.Lock()
			detached = guard
			originalSaved := saved
			parent, guard, failSave = false, false, false
			before := patches
			mu.Unlock()

			run(0, "apply", "-auto-approve", "-no-color")
			if state := run(0, "state", "list"); strings.Contains(state, "fastiron_spanning_tree_interface.test") {
				t.Fatalf("state retained absent policy: %s", state)
			}
			mu.Lock()
			if patches != before {
				t.Error("deleted parent received an STP mutation")
			}
			if !pending && saved != originalSaved {
				t.Error("refresh saved an external configuration change")
			}
			if pending && saved != "ver 09.0.10k\nend" {
				t.Errorf("pending save was lost: %q", saved)
			}
			mu.Unlock()
			run(0, "plan", "-detailed-exitcode", "-no-color")

			write("main.tf", base+`resource "fastiron_spanning_tree_interface" "test" { interface = "lag 11" }`)
			run(1, "plan", "-no-color")
		})
	}
}
