package ve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestEmptyInterfaceDatabase(t *testing.T) {
	// A RESTCONF restart returned these shapes despite native physical and
	// logical interfaces still existing. They cannot establish resource absence.
	for _, body := range []string{
		`{"openconfig-interfaces:interfaces":{}}`,
		`{"openconfig-interfaces:interfaces":{"interface":[]}}`,
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, body)
		}))
		t.Cleanup(server.Close)
		device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := Read(context.Background(), device, 53); err == nil || errors.Is(err, fastiron.ErrNotFound) {
			t.Fatalf("incomplete database reported as confirmed state: %v", err)
		}
	}
}

func TestNativeRead(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		id                   int64
		native, cached, want string
		absent, invalid      bool
	}{
		{name: "minimum identity", id: 1, native: "interface ve 1\n port-name EDGE\n", cached: `{"name":"ve 1","config":{"name":"ve 1","type":"iana-if-type:l3ipvlan","description":"EDGE"},"openconfig-vlan:routed-vlan":{"config":{"vlan":1}}}`, want: "EDGE"},
		{name: "maximum identity", id: 4095, native: "interface ve 4095\n", cached: `{"name":"ve 4095","config":{"name":"ve 4095","type":"iana-if-type:l3ipvlan","description":"STALE"},"openconfig-vlan:routed-vlan":{"config":{"vlan":4095}}}`},
		{name: "CLI drift", id: 53, native: "interface ve 53\n port-name DRIFT\n", cached: `{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan","description":"CACHED"},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`, want: "DRIFT"},
		{name: "native only", id: 53, native: "interface ve 53\n port-name NATIVE\n", want: "NATIVE"},
		{name: "cached only", id: 53, cached: `{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan","description":"CACHED"},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`, absent: true},
		{name: "absent", id: 53, absent: true},
		{name: "zero identity", id: 0, invalid: true},
		{name: "oversized identity", id: 4096, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show running-config":
					return "ver 09.0.10kT213\n" + tc.native + "end"
				default:
					t.Errorf("unexpected native command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/interfaces", func(w http.ResponseWriter, r *http.Request) {
				if tc.invalid || r.Method != http.MethodGet {
					t.Errorf("unexpected request %s", r.Method)
				}
				entries := `{"name":"ethernet 1/1/1"}`
				if tc.cached != "" {
					entries += "," + tc.cached
				}
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[%s]}}`, entries)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "manual",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}

			observed, err := Read(context.Background(), device, tc.id)
			if tc.invalid {
				if err == nil || errors.Is(err, fastiron.ErrNotFound) {
					t.Fatalf("invalid identity accepted: %v", err)
				}
				return
			}
			if tc.absent {
				if !errors.Is(err, fastiron.ErrNotFound) {
					t.Fatalf("absence error=%v", err)
				}
				return
			}
			if err != nil || observed.ID != tc.id || observed.VLANID != tc.id || observed.PortName != tc.want {
				t.Fatalf("VE=%+v error=%v", observed, err)
			}
		})
	}
}

func TestAmbiguousIdentity(t *testing.T) {
	const entry = `{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan"},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`
	for name, entries := range map[string]string{
		"duplicate":      entry + "," + entry,
		"different VLAN": `{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan"},"openconfig-vlan:routed-vlan":{"config":{"vlan":54}}}`,
		"missing VLAN":   `{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan"},"openconfig-vlan:routed-vlan":{"config":{}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[%s]}}`, entries)
			}))
			t.Cleanup(server.Close)
			device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			_, err = Read(context.Background(), device, 53)
			if err == nil || errors.Is(err, fastiron.ErrNotFound) {
				t.Fatalf("ambiguous VE identity reported as confirmed state: %v", err)
			}
		})
	}
}
