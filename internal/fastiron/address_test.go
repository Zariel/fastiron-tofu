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

func TestInterfaceAddresses(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		failure          bool
	}{
		{"host bits", `{"openconfig-if-ip:addresses":{"address":[{"ip":"192.0.2.129","config":{"ip":"192.0.2.129","prefix-length":24}}]}}`, "192.0.2.129/24", false},
		{"empty", `{"openconfig-if-ip:addresses":{}}`, "", false},
		{"missing collection", `{}`, "", true},
		{"missing prefix", `{"openconfig-if-ip:addresses":{"address":[{"ip":"192.0.2.129","config":{"ip":"192.0.2.129"}}]}}`, "", true},
		{"identity mismatch", `{"openconfig-if-ip:addresses":{"address":[{"ip":"192.0.2.129","config":{"ip":"192.0.2.130","prefix-length":24}}]}}`, "", true},
		{"wrong family", `{"openconfig-if-ip:addresses":{"address":[{"ip":"2001:db8::1","config":{"ip":"2001:db8::1","prefix-length":64}}]}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			d, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			addresses, err := d.InterfaceAddresses(context.Background(), "ve 53", false)
			if (err != nil) != tc.failure {
				t.Fatalf("addresses=%v error=%v", addresses, err)
			}
			if tc.want != "" {
				if len(addresses) != 1 || addresses[0].String() != tc.want {
					t.Fatalf("addresses=%v want %s", addresses, tc.want)
				}
			} else if len(addresses) != 0 {
				t.Fatalf("unexpected addresses: %v", addresses)
			}
		})
	}
}
