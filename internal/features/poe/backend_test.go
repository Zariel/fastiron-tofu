package poe

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
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestPoEConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, body, native string
		enabled, failure   bool
	}{
		{"default with stale state", `{"icx-openconfig-if-poe-aug:poe":{"config":{},"state":{"enabled":false}}}`, "", true, false},
		{"disabled with live state", `{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":false},"state":{"enabled":true}}}`, " no inline power\n", false, false},
		{"stale cache enabled", `{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":true},"state":{"enabled":true}}}`, " no inline power\n", false, false},
		{"stale cache disabled", `{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":false},"state":{"enabled":false}}}`, " inline power priority 1\n", true, false},
		{"unsupported", `{}`, "", false, true},
		{"missing configuration", `{"icx-openconfig-if-poe-aug:poe":{"state":{"enabled":true}}}`, "", false, true},
		{"malformed native", `{"icx-openconfig-if-poe-aug:poe":{"config":{}}}`, " inline power priority\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show running-config":
					return "ver 09.0.10k\ninterface ethernet 1/1/12\n" + tc.native + "end"
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := readPort(context.Background(), device, "ethernet 1/1/12")
			if (err != nil) != tc.failure || p.Enabled != tc.enabled {
				t.Fatalf("PoE=%+v error=%v", p, err)
			}
		})
	}
}

func TestEmptyInterfaceDatabase(t *testing.T) {
	for _, body := range []string{`{"openconfig-interfaces:interfaces":{}}`, `{"openconfig-interfaces:interfaces":{"interface":[]}}`} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		t.Cleanup(server.Close)
		device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readPorts(context.Background(), device); err == nil || errors.Is(err, fastiron.ErrNotFound) {
			t.Fatalf("incomplete database reported as confirmed state: %v", err)
		}
	}
}

func TestAllocationOwnership(t *testing.T) {
	for _, setting := range []string{"priority 1", "power-by-class 2", "power-limit 20000"} {
		t.Run(setting, func(t *testing.T) {
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return "ver 09.0.10k\ninterface ethernet 1/1/12\n inline power " + setting + "\nend"
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("mutated independently owned allocation: %s", r.Method)
				}
				fmt.Fprint(w, `{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":true}}}`)
			})
			device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := applyPort(context.Background(), device, "ethernet 1/1/12", false)
			if err == nil || observed == nil || !observed.Enabled {
				t.Fatalf("allocation guard: observed=%+v error=%v", observed, err)
			}
		})
	}
}
