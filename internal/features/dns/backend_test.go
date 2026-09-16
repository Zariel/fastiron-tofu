package dns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestServers(t *testing.T) {
	// A readable empty collection is absence; an unsupported or malformed
	// response must not make OpenTofu forget a configured server.
	for _, tc := range []struct {
		name, body string
		want       []string
		failure    bool
	}{
		{"empty", `{"openconfig-system:dns":{"servers":{}}}`, []string{}, false},
		{"configured", `{"openconfig-system:dns":{"servers":{"server":[{"address":"192.0.2.53","config":{"address":"192.0.2.53"}}]}}}`, []string{"192.0.2.53"}, false},
		{"missing container", `{}`, nil, true},
		{"missing servers", `{"openconfig-system:dns":{}}`, nil, true},
		{"missing config", `{"openconfig-system:dns":{"servers":{"server":[{"address":"192.0.2.53"}]}}}`, nil, true},
		{"identity mismatch", `{"openconfig-system:dns":{"servers":{"server":[{"address":"192.0.2.53","config":{"address":"192.0.2.54"}}]}}}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/system/dns" {
					http.Error(w, "wrong operation", 400)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := cachedServers(context.Background(), device)
			if (err != nil) != tc.failure || !slices.Equal(got, tc.want) {
				t.Fatalf("servers=%v error=%v", got, err)
			}
		})
	}
}
