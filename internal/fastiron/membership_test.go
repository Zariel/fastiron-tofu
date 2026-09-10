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

func TestMembershipInterfaces(t *testing.T) {
	for _, name := range []string{"lag 0", "lag 01", "lag -1", "lag 1/2", "ve 5", "ethernet 01/1/2", "ethernet 1/1/2\n"} {
		if err := ValidateVLANMembership(VLANMembership{VLANID: 53, Interface: name, Tagging: "tagged"}); err == nil {
			t.Errorf("accepted invalid switchport %q", name)
		}
	}
	for _, tc := range []struct{ name, path string }{
		{"ethernet 1/1/2", "/interfaces/interface=ethernet%201%2F1%2F2/ethernet/switched-vlan"},
		{"lag 53", "/interfaces/interface=lag%2053/aggregation/switched-vlan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.EscapedPath() != tc.path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.EscapedPath())
					w.WriteHeader(404)
					return
				}
				fmt.Fprint(w, `{"openconfig-vlan:switched-vlan":{"config":{"access-vlan":54,"trunk-vlans":[53,55]}}}`)
			}))
			defer server.Close()
			device, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			for _, membership := range []VLANMembership{{VLANID: 53, Interface: tc.name, Tagging: "tagged"}, {VLANID: 54, Interface: tc.name, Tagging: "untagged"}} {
				present, err := device.VLANMembership(context.Background(), membership)
				if err != nil || !present {
					t.Fatalf("membership=%#v present=%v error=%v", membership, present, err)
				}
			}
		})
	}
}
