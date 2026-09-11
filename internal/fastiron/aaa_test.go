package fastiron

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestAAAServers(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []AAAServer
		failure    bool
	}{
		{"empty", `{"openconfig-system:server-groups":{}}`, []AAAServer{}, false},
		{"empty group", `{"openconfig-system:server-groups":{"server-group":[{"name":"tacacs-default-group","config":{"name":"tacacs-default-group","type":"openconfig-aaa:TACACS"},"servers":{}}]}}`, []AAAServer{}, false},
		{"radius", `{"openconfig-system:server-groups":{"server-group":[{"name":"radius-default-group","config":{"name":"radius-default-group","type":"openconfig-aaa:RADIUS"},"servers":{"server":[{"address":"192.0.2.53","config":{"address":"192.0.2.53"},"radius":{"config":{"auth-port":1912,"acct-port":1913,"secret-key":"opaque-test-value","icx-openconfig-aaa-aug:purpose":"accounting-only"}}}]}}]}}`, []AAAServer{{Kind: "radius", Address: "192.0.2.53", AuthPort: 1912, AcctPort: 1913, Purpose: "accounting-only"}}, false},
		{"tacacs", `{"openconfig-system:server-groups":{"server-group":[{"name":"tacacs-default-group","config":{"name":"tacacs-default-group","type":"openconfig-aaa:TACACS"},"servers":{"server":[{"address":"192.0.2.54","config":{"address":"192.0.2.54"},"tacacs":{"config":{"port":49,"secret-key":"","icx-openconfig-aaa-aug:purpose":"default"}}}]}}]}}`, []AAAServer{{Kind: "tacacs", Address: "192.0.2.54", AuthPort: 49, Purpose: "default"}}, false},
		{"missing root", `{}`, nil, true},
		{"group mismatch", `{"openconfig-system:server-groups":{"server-group":[{"name":"radius-default-group","config":{"name":"tacacs-default-group","type":"openconfig-aaa:RADIUS"},"servers":{}}]}}`, nil, true},
		{"missing servers", `{"openconfig-system:server-groups":{"server-group":[{"name":"radius-default-group","config":{"name":"radius-default-group","type":"openconfig-aaa:RADIUS"}}]}}`, nil, true},
		{"server mismatch", `{"openconfig-system:server-groups":{"server-group":[{"name":"radius-default-group","config":{"name":"radius-default-group","type":"openconfig-aaa:RADIUS"},"servers":{"server":[{"address":"192.0.2.53","config":{"address":"192.0.2.54"}}]}}]}}`, nil, true},
		{"missing protocol", `{"openconfig-system:server-groups":{"server-group":[{"name":"radius-default-group","config":{"name":"radius-default-group","type":"openconfig-aaa:RADIUS"},"servers":{"server":[{"address":"192.0.2.53","config":{"address":"192.0.2.53"}}]}}]}}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/restconf/data/system/aaa/server-groups" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			d, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			got, err := d.AAAServers(context.Background())
			if (err != nil) != tc.failure {
				t.Fatalf("servers=%v error=%v", got, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("servers=%v want=%v", got, tc.want)
			}
		})
	}
}
