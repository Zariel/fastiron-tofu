package fastiron_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestL2Owner(t *testing.T) {
	const port = `{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9"}}`
	const lag = `{"name":"lag 53","config":{"name":"lag 53"}}`
	const member = `{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 53"}}}`
	const prefix = `{"openconfig-interfaces:interfaces":{"interface":[`
	const suffix = `]}}`
	for _, tc := range []struct {
		name, target, response, outcome, native string
	}{
		{"Ethernet", "ethernet 1/1/9", prefix + port + suffix, "valid", ""},
		{"LAG", "lag 53", prefix + member + "," + lag + suffix, "valid", ""},
		{"member", "ethernet 1/1/9", prefix + member + "," + lag + suffix, "member", ""},
		{"dangling member", "ethernet 1/1/9", prefix + member + suffix, "member", ""},
		{"absent", "ethernet 1/1/9", prefix + lag + suffix, "absent", ""},
		{"missing collection", "ethernet 1/1/9", `{}`, "invalid", ""},
		{"empty collection", "ethernet 1/1/9", prefix + suffix, "invalid", ""},
		{"duplicate", "ethernet 1/1/9", prefix + port + "," + port + suffix, "invalid", ""},
		{"missing identity", "ethernet 1/1/9", prefix + `{"name":"ethernet 1/1/9"}` + suffix, "invalid", ""},
		{"inconsistent identity", "ethernet 1/1/9", prefix + `{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/10"}}` + suffix, "invalid", ""},
		{"unrelated incomplete identity", "ethernet 1/1/9", prefix + `{"name":"ethernet 1/1/10"},` + port + suffix, "valid", ""},
		{"cached deleted LAG", "lag 53", prefix + lag + suffix, "absent", "ver 09.0.10k\ninterface ethernet 1/1/9\n stp-bpdu-guard\nend"},
		{"uncached deleted LAG", "lag 53", prefix + port + suffix, "absent", "ver 09.0.10k\nend"},
		{"undiscovered native LAG", "lag 53", prefix + port + suffix, "invalid", ""},
		{"malformed native LAG", "lag 53", prefix + lag + suffix, "invalid", "ver 09.0.10k\nlag test static\nend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native := tc.native
			if native == "" {
				native = "ver 09.0.10k\nlag test static id 53\nend"
			}
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return native
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("GET /restconf/data/interfaces", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, tc.response) })
			device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			err = device.CheckL2Owner(context.Background(), tc.target)
			switch tc.outcome {
			case "valid":
				if err != nil {
					t.Fatal(err)
				}
			case "absent":
				if !errors.Is(err, fastiron.ErrNotFound) {
					t.Fatalf("absence error: %v", err)
				}
			default:
				// Uncertain identity or ownership must not remove a resource from state.
				if err == nil || errors.Is(err, fastiron.ErrNotFound) {
					t.Fatalf("expected a blocking error, got %v", err)
				}
				if tc.outcome == "member" && !strings.Contains(err.Error(), "lag 53") {
					t.Fatalf("missing owner in diagnostic: %v", err)
				}
			}
		})
	}
}
