package stp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestMember(t *testing.T) {
	for _, name := range []string{"ethernet 1/1/9", "ethernet 1/1/10"} {
		t.Run(name, func(t *testing.T) {
			var mutations atomic.Int32
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config", "show configuration":
					return "ver 09.0.10k\nlag test static id 11\n ports ethe 1/1/9 to 1/1/10\nend"
				case "write memory":
					mutations.Add(1)
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("GET /interfaces", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":%q,"config":{"name":%q},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 11"}}}]}}`, name, name)
			})
			server.HandleFunc("/stp/interfaces", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{}}`)
					return
				}
				// The switch can acknowledge member writes without changing native policy.
				mutations.Add(1)
				w.WriteHeader(http.StatusNoContent)
			})
			device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			_, err = readInterface(context.Background(), device, name)
			if err == nil || !strings.Contains(err.Error(), "LAG member") {
				t.Fatalf("read should reject membership: %v", err)
			}
			_, err = applyInterface(context.Background(), device, name, interfaceConfig{BPDUGuard: true}, true)
			if err == nil || !strings.Contains(err.Error(), "LAG member") {
				t.Fatalf("write should reject membership: %v", err)
			}
			if mutations.Load() != 0 {
				t.Fatalf("member operation mutated the switch %d times", mutations.Load())
			}
		})
	}
}
