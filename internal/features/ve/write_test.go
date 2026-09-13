package ve

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestPartialWrite(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		t.Run(fmt.Sprintf("delete=%t", deletion), func(t *testing.T) {
			var mu sync.Mutex
			exists, name := true, "OLD"
			writes, saves := 0, 0
			running := func() string {
				config := "ver 09.0.10kT213\nvlan 53 name TRANSIT by port\n"
				if exists {
					config += " router-interface ve 53\ninterface ve 53\n port-name " + name + "\n"
				}
				return config + "end"
			}
			startup := running()
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return running()
				case "show configuration":
					return startup
				case "write memory":
					saves++
					startup = running()
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method != http.MethodGet {
					writes++
					if deletion {
						exists = false
					} else {
						name = "NEW"
					}
					http.Error(w, "injected partial write failure", http.StatusInternalServerError)
					return
				}
				switch r.URL.Path {
				case "/network-instances/network-instance=default-vrf/vlans/vlan=53":
					fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":53,"config":{"vlan-id":53,"name":"TRANSIT"}}]}`)
				case "/interfaces":
					entry := ""
					if exists {
						entry = fmt.Sprintf(`,{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan","description":%q},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`, name)
					}
					fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"}%s]}}`, entry)
				default:
					t.Errorf("unexpected path %q", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "after_each_write",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}
			operation := func() error {
				if deletion {
					return remove(context.Background(), device, 53)
				}
				observed, err := apply(context.Background(), device, config{ID: 53, VLANID: 53, PortName: "NEW"})
				if observed == nil || observed.PortName != "NEW" {
					t.Fatalf("lost observed partial write: %+v", observed)
				}
				return err
			}

			if err := operation(); err == nil {
				t.Fatal("partial write failure reported as success")
			}
			mu.Lock()
			saved, attempts := saves, writes
			mu.Unlock()
			if saved != 0 || attempts != 1 {
				t.Fatalf("saves=%d writes=%d after failure", saved, attempts)
			}

			if err := operation(); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if saves != 1 || writes != 1 || startup != running() {
				t.Fatalf("retry did not persist observed configuration without another mutation: saves=%d writes=%d", saves, writes)
			}
		})
	}
}
