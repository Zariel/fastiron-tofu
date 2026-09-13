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

func TestDelete(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		absent, child, ignore, corrupt, stall bool
	}{
		{name: "delete"},
		{name: "already absent", absent: true},
		{name: "unowned child", child: true},
		{name: "false acknowledgement", ignore: true},
		{name: "unowned mutation", corrupt: true},
		{name: "stalled cache", stall: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			exists, cached := !tc.absent, true
			neighbor := "NEIGHBOR"
			writes, saves, reads := 0, 0, 0
			running := func() string {
				text := "ver 09.0.10kT213\nvlan 53 name TRANSIT by port\ninterface ethernet 1/1/1\n port-name " + neighbor + "\n"
				if exists {
					text += "interface ve 53\n port-name OWNED\n"
					if tc.child {
						text += " disable\n"
					}
				}
				return text + "end"
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
				if r.Method == http.MethodDelete && r.URL.Path == "/interfaces/interface=ve 53" {
					writes++
					if !tc.ignore {
						exists = false
					}
					if tc.corrupt {
						neighbor = "CHANGED"
					}
					w.WriteHeader(204)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/interfaces" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(405)
					return
				}
				reads++
				if reads >= 3 && !tc.stall {
					cached = exists
				}
				entry := ""
				if cached {
					entry = `,{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan","description":"OWNED"},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`
				}
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"}%s]}}`, entry)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "after_each_write",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}

			err = remove(context.Background(), device, 53)
			failure := tc.child || tc.ignore || tc.corrupt || tc.stall
			if (err != nil) != failure {
				t.Fatalf("error=%v", err)
			}
			mu.Lock()
			saved, mutations := saves, writes
			mu.Unlock()
			if failure {
				if saved != 0 {
					t.Fatal("saved an unsafe or unverified deletion")
				}
				if tc.child && mutations != 0 {
					t.Fatal("deleted an interface with unowned children")
				}
				return
			}
			if err := remove(context.Background(), device, 53); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			expectedWrites := 1
			if tc.absent {
				expectedWrites = 0
			}
			if writes != expectedWrites || exists || cached || saves != 2 {
				t.Fatalf("writes=%d exists=%t cached=%t saves=%d", writes, exists, cached, saves)
			}
			const want = "ver 09.0.10kT213\nvlan 53 name TRANSIT by port\ninterface ethernet 1/1/1\n port-name NEIGHBOR\nend"
			if startup != want {
				t.Fatalf("saved configuration=%q", startup)
			}
		})
	}
}
