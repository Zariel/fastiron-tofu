package ospf

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

func TestOSPFAreas(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		failure          bool
	}{
		{"decimal backbone", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":0},"interfaces":{}}]}}`, "0.0.0.0", false},
		{"dotted", `{"openconfig-network-instance:areas":{"area":[{"identifier":"0.0.0.53","config":{"identifier":"0.0.0.53"},"interfaces":{}}]}}`, "0.0.0.53", false},
		{"equivalent representations", `{"openconfig-network-instance:areas":{"area":[{"identifier":53,"config":{"identifier":"0.0.0.53"},"interfaces":{}}]}}`, "0.0.0.53", false},
		{"full identifier range", `{"openconfig-network-instance:areas":{"area":[{"identifier":4294967295,"config":{"identifier":4294967295},"interfaces":{}}]}}`, "255.255.255.255", false},
		{"empty", `{"openconfig-network-instance:areas":{}}`, "", false},
		{"missing collection", `{}`, "", true},
		{"missing interface collection", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":0}}]}}`, "", true},
		{"conflicting identity", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":53},"interfaces":{}}]}}`, "", true},
		{"duplicate normalized identity", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":0},"interfaces":{}},{"identifier":"0.0.0.0","config":{"identifier":"0.0.0.0"},"interfaces":{}}]}}`, "", true},
		{"binding identity mismatch", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":0},"interfaces":{"interface":[{"id":"ve 5","config":{"id":"ve 53"}}]}}]}}`, "", true},
		{"loopback binding", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":0},"interfaces":{"interface":[{"id":"loopback 32","config":{"id":"loopback 32"}}]}}]}}`, "0.0.0.0", false},
		{"noncanonical loopback", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":0},"interfaces":{"interface":[{"id":"loopback 032","config":{"id":"loopback 032"}}]}}]}}`, "", true},
		{"zero loopback", `{"openconfig-network-instance:areas":{"area":[{"identifier":0,"config":{"identifier":0},"interfaces":{"interface":[{"id":"loopback 0","config":{"id":"loopback 0"}}]}}]}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			areas, err := cachedAreas(context.Background(), d)
			if (err != nil) != tc.failure {
				t.Fatalf("areas=%v error=%v", areas, err)
			}
			if tc.want != "" {
				if len(areas) != 1 || areas[0].ID != tc.want {
					t.Fatalf("areas=%v want=%s", areas, tc.want)
				}
			} else if len(areas) != 0 {
				t.Fatalf("unexpected areas=%v", areas)
			}
		})
	}
}
