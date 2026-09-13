package lldp

import (
	"context"
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
				fmt.Fprint(w, `{"openconfig-lldp:interface":[{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11","enabled":false}}]}`)
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
