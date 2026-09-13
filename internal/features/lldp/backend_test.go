package lldp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestLLDPDefaults(t *testing.T) {
	// Missing defaulted leaves and missing capability containers are distinct.
	for _, tc := range []struct {
		name, iface, body string
		enabled, failure  bool
	}{
		{"global default", "", `{"openconfig-lldp:config":{}}`, true, false},
		{"global disabled", "", `{"openconfig-lldp:config":{"enabled":false}}`, false, false},
		{"global unsupported", "", `{}`, false, true},
		{"interface default", "ethernet 1/1/2", `{"openconfig-lldp:interface":[{"name":"ethernet 1/1/2","config":{"name":"ethernet 1/1/2"}}]}`, true, false},
		{"interface mismatch", "ethernet 1/1/2", `{"openconfig-lldp:interface":[{"name":"ethernet 1/1/3","config":{"name":"ethernet 1/1/3","enabled":true}}]}`, false, true},
		{"interface missing config", "ethernet 1/1/2", `{"openconfig-lldp:interface":[{"name":"ethernet 1/1/2"}]}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			enabled, err := readRESTEnabled(context.Background(), device, tc.iface)
			if (err != nil) != tc.failure || enabled != tc.enabled {
				t.Fatalf("enabled=%v error=%v", enabled, err)
			}
		})
	}
}
