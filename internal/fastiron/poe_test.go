package fastiron

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestPoEConfiguration(t *testing.T) {
	// Operational enabled is not a substitute for configured enable state.
	for _, tc := range []struct {
		name, body       string
		enabled, failure bool
	}{
		{"default with stale state", `{"icx-openconfig-if-poe-aug:poe":{"config":{},"state":{"enabled":false}}}`, true, false},
		{"disabled with live state", `{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":false},"state":{"enabled":true}}}`, false, false},
		{"unsupported", `{}`, false, true},
		{"missing configuration", `{"icx-openconfig-if-poe-aug:poe":{"state":{"enabled":true}}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			device, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := device.PoE(context.Background(), "ethernet 1/1/12")
			if (err != nil) != tc.failure || p.Enabled != tc.enabled {
				t.Fatalf("PoE=%+v error=%v", p, err)
			}
		})
	}
}
