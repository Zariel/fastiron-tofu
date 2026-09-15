package stp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestRESTInterfaces(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       map[string]interfaceConfig
		failure    bool
	}{
		{"empty", `{"openconfig-spanning-tree:interfaces":{}}`, map[string]interfaceConfig{}, false},
		{"enabled", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","edge-port":"openconfig-spanning-tree-types:EDGE_ENABLE","guard":"ROOT","bpdu-guard":true},"state":{}}]}}`, map[string]interfaceConfig{"ethernet 1/1/12": {AdminEdge: true, RootGuard: true, BPDUGuard: true}}, false},
		{"explicit defaults", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","edge-port":"openconfig-spanning-tree-types:EDGE_DISABLE","guard":"NONE","bpdu-guard":false},"state":{}}]}}`, map[string]interfaceConfig{"ethernet 1/1/12": {}}, false},
		{"omitted defaults", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","bpdu-guard":true},"state":{}}]}}`, map[string]interfaceConfig{"ethernet 1/1/12": {BPDUGuard: true}}, false},
		{"config not state", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"},"state":{"bpdu-guard":true,"guard":"ROOT","edge-port":"openconfig-spanning-tree-types:EDGE_ENABLE"}}]}}`, map[string]interfaceConfig{"ethernet 1/1/12": {}}, false},
		{"missing container", `{}`, nil, true},
		{"missing config", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12"}]}}`, nil, true},
		{"identity mismatch", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/11"}}]}}`, nil, true},
		{"duplicate", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}},{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}}]}}`, nil, true},
		{"unknown edge mode", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","edge-port":"EDGE_AUTO"}}]}}`, nil, true},
		{"unknown guard", `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","guard":"LOOP"}}]}}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/restconf/data/stp/interfaces" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			got, err := readRESTInterfaces(context.Background(), d)
			if (err != nil) != tc.failure {
				t.Fatalf("interfaces=%v error=%v", got, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("interfaces=%v want=%v", got, tc.want)
			}
		})
	}
}
