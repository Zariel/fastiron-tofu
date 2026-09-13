package lldp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestPortPreservation(t *testing.T) {
	for _, change := range []string{"regroup", "direction", "unowned", "echo", "partial"} {
		t.Run(change, func(t *testing.T) {
			fixture := func(name string) string {
				t.Helper()
				data, err := os.ReadFile(filepath.Join("..", "..", "config", "testdata", "lldp", name))
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			before, after := fixture("range-disabled.conf"), fixture("split-range.conf")
			if change == "direction" {
				before, after = fixture("mixed-modes.conf"), fixture("mixed-regrouped.conf")
				after = strings.Replace(after, "no lldp enable transmit ports ethe 1/1/10", "no lldp enable receive ports ethe 1/1/10", 1)
			}
			if change == "unowned" {
				after = strings.Replace(after, "global-stp", "no global-stp", 1)
			}
			if change == "echo" {
				after = before
			}
			inventory := fixture("range-disabled.rest.json")
			var mu sync.Mutex
			running, saved := before, before
			saves := 0
			cached := false
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return running
				case "show configuration":
					return saved
				case "write memory":
					saves++
					saved = running
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/lldp/interfaces", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.Method {
				case http.MethodGet:
					fmt.Fprint(w, inventory)
				case http.MethodPatch:
					running = after
					cached = change != "direction"
					if change == "partial" {
						http.Error(w, "partial mutation", 500)
						return
					}
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
				}
			})
			server.HandleFunc("/lldp/interfaces/interface=ethernet%201%2F1%2F11", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method %s", r.Method)
				}
				mu.Lock()
				defer mu.Unlock()
				fmt.Fprintf(w, `{"openconfig-lldp:interface":[{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11","enabled":%t}}]}`, cached)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "after_each_write",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}

			desired := change != "direction"
			observed, err := applyEnabled(context.Background(), device, "ethernet 1/1/11", desired)
			mu.Lock()
			defer mu.Unlock()
			if change == "regroup" {
				if err != nil || observed == nil || !*observed || saves != 1 || saved != after {
					t.Fatalf("regroup: observed=%v error=%v saves=%d", observed, err, saves)
				}
				return
			}
			if err == nil || saves != 0 || saved != before {
				t.Fatalf("unsafe save: observed=%v error=%v saves=%d", observed, err, saves)
			}
			if (change == "direction" || change == "unowned") && !strings.Contains(err.Error(), "unrelated configuration") {
				t.Fatalf("missing preservation diagnostic: %v", err)
			}
		})
	}
}

func TestPortSynchronization(t *testing.T) {
	for _, failure := range []string{"", "synchronization", "mutation"} {
		t.Run(failure, func(t *testing.T) {
			var mu sync.Mutex
			cached := false
			running := "ver 09.0.10kT213\nno lldp enable transmit ports ethe 1/1/11\nend"
			original := running
			saved := original
			writes, saves := 0, 0
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return running
				case "show configuration":
					return saved
				case "write memory":
					saves++
					saved = running
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			respond := func(w http.ResponseWriter, collection bool) {
				entry := fmt.Sprintf(`{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11","enabled":%t}}`, cached)
				if collection {
					fmt.Fprintf(w, `{"openconfig-lldp:interfaces":{"interface":[%s]}}`, entry)
					return
				}
				fmt.Fprintf(w, `{"openconfig-lldp:interface":[%s]}`, entry)
			}
			server.HandleFunc("/lldp/interfaces/interface=ethernet%201%2F1%2F11", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method %s", r.Method)
				}
				respond(w, false)
			})
			server.HandleFunc("/lldp/interfaces", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					respond(w, true)
					return
				}
				if r.Method != http.MethodPatch {
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
					return
				}
				var body struct {
					Interfaces struct {
						Interface []struct{ Config struct{ Enabled bool } }
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Interfaces.Interface) != 1 {
					t.Errorf("invalid payload: %v", err)
					w.WriteHeader(400)
					return
				}
				desired := body.Interfaces.Interface[0].Config.Enabled
				writes++
				if cached != desired {
					cached = desired
					running = "ver 09.0.10kT213\nend"
					if !desired {
						running = "ver 09.0.10kT213\nno lldp enable ports ethe 1/1/11\nend"
					}
				}
				if (failure == "synchronization" && writes == 1) || (failure == "mutation" && writes == 2) {
					http.Error(w, "partial failure", 500)
					return
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
			observed, err := applyEnabled(context.Background(), device, "ethernet 1/1/11", false)
			mu.Lock()
			defer mu.Unlock()
			if failure == "" {
				if err != nil || observed == nil || *observed || saves != 1 || saved != "ver 09.0.10kT213\nno lldp enable ports ethe 1/1/11\nend" {
					t.Fatalf("convergence: observed=%v error=%v saves=%d", observed, err, saves)
				}
				return
			}
			if err == nil || saves != 0 || saved != original {
				t.Fatalf("failed write saved: error=%v saves=%d", err, saves)
			}
			if failure == "synchronization" && (writes != 1 || observed == nil || !*observed) {
				t.Fatalf("continued after synchronization failure: writes=%d observed=%v", writes, observed)
			}

			mu.Unlock()
			observed, err = applyEnabled(context.Background(), device, "ethernet 1/1/11", false)
			mu.Lock()
			if err != nil || observed == nil || *observed || saves != 1 || saved != "ver 09.0.10kT213\nno lldp enable ports ethe 1/1/11\nend" {
				t.Fatalf("retry: observed=%v error=%v saves=%d", observed, err, saves)
			}
			if writes != 2 {
				t.Fatalf("retry repeated a completed mutation: writes=%d", writes)
			}
		})
	}
}
