package fastiron_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestL2Owner(t *testing.T) {
	const port = `{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9"}}`
	const lag = `{"name":"lag 53","config":{"name":"lag 53"}}`
	const member = `{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 53"}}}`
	const prefix = `{"openconfig-interfaces:interfaces":{"interface":[`
	const suffix = `]}}`
	for _, tc := range []struct {
		name, target, response, outcome string
	}{
		{"Ethernet", "ethernet 1/1/9", prefix + port + suffix, "valid"},
		{"LAG", "lag 53", prefix + member + "," + lag + suffix, "valid"},
		{"member", "ethernet 1/1/9", prefix + member + "," + lag + suffix, "member"},
		{"dangling member", "ethernet 1/1/9", prefix + member + suffix, "member"},
		{"absent", "ethernet 1/1/9", prefix + lag + suffix, "absent"},
		{"missing collection", "ethernet 1/1/9", `{}`, "invalid"},
		{"empty collection", "ethernet 1/1/9", prefix + suffix, "invalid"},
		{"duplicate", "ethernet 1/1/9", prefix + port + "," + port + suffix, "invalid"},
		{"missing identity", "ethernet 1/1/9", prefix + `{"name":"ethernet 1/1/9"}` + suffix, "invalid"},
		{"inconsistent identity", "ethernet 1/1/9", prefix + `{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/10"}}` + suffix, "invalid"},
		{"unrelated incomplete identity", "ethernet 1/1/9", prefix + `{"name":"ethernet 1/1/10"},` + port + suffix, "valid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /restconf/data/interfaces", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tc.response)
			})
			server := httptest.NewTLSServer(mux)
			defer server.Close()
			device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
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
