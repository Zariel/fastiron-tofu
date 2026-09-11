package stp

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

func TestSTPVLANs(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       *vlan
		failure    bool
	}{
		{"rstp priority", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{"vlan":[{"vlan-id":53,"config":{"vlan-id":53,"bridge-priority":12345}}]},"icx-openconfig-spanning-tree-aug:pvst":{}}}`, &vlan{VLANID: 53, Mode: "rstp", Priority: 12345}, false},
		{"default VLAN", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{"vlan":[{"vlan-id":1,"config":{"vlan-id":1,"pvst-priority":32768}}]}}}`, &vlan{VLANID: 1, Mode: "stp", Priority: 32768}, false},
		{"classic default", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{"vlan":[{"vlan-id":53,"config":{"vlan-id":53}}]}}}`, &vlan{VLANID: 53, Mode: "stp", Priority: 32768}, false},
		{"zero priority", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{"vlan":[{"vlan-id":53,"config":{"vlan-id":53,"pvst-priority":0}}]}}}`, &vlan{VLANID: 53, Mode: "stp", Priority: 0}, false},
		{"empty", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{}}}`, nil, false},
		{"missing root", `{}`, nil, true},
		{"missing mode", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{}}}`, nil, true},
		{"missing config", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{"vlan":[{"vlan-id":53}]},"icx-openconfig-spanning-tree-aug:pvst":{}}}`, nil, true},
		{"identity mismatch", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{"vlan":[{"vlan-id":53,"config":{"vlan-id":54}}]},"icx-openconfig-spanning-tree-aug:pvst":{}}}`, nil, true},
		{"conflicting modes", `{"openconfig-spanning-tree:stp":{"rapid-pvst":{"vlan":[{"vlan-id":53,"config":{"vlan-id":53}}]},"icx-openconfig-spanning-tree-aug:pvst":{"vlan":[{"vlan-id":53,"config":{"vlan-id":53}}]}}}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			vlans, err := readVLANs(context.Background(), d)
			if (err != nil) != tc.failure {
				t.Fatalf("vlans=%v error=%v", vlans, err)
			}
			if tc.want != nil {
				if len(vlans) != 1 || vlans[0] != *tc.want {
					t.Fatalf("vlans=%v want=%v", vlans, *tc.want)
				}
			} else if len(vlans) != 0 {
				t.Fatalf("unexpected VLANs=%v", vlans)
			}
		})
	}
}
