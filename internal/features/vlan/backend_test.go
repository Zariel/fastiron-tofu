package vlan

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

func TestVLANChildren(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		blocked      bool
	}{
		{"empty domain", "ver 09.0.10k\nvlan 53 name INFRA by port\n!\nend", false},
		{"membership", "ver 09.0.10k\nvlan 53 by port\n tagged ethe 1/1/1\n!\nend", true},
		{"routed interface", "ver 09.0.10k\nvlan 53 by port\n!\ninterface ve 53\n!\nend", true},
		{"unrelated VLAN", "ver 09.0.10k\nvlan 54 by port\n tagged ethe 1/1/1\n!\nend", false},
		{"truncated", "ver 09.0.10k\nvlan 53", true},
		{"unrecognized output", "not a configuration", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := vlanChildren(tc.config, 53); (err != nil) != tc.blocked {
				t.Fatalf("child check: %v", err)
			}
		})
	}
}

func TestReadDefaultVLAN(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":1,"config":{"vlan-id":1,"name":"DEFAULT-VLAN"}}]}`)
	}))
	defer server.Close()
	d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}

	vlan, err := Read(context.Background(), d, 1)
	if err != nil || vlan.ID != 1 || vlan.Name != "DEFAULT-VLAN" {
		t.Fatalf("default VLAN=%v error=%v", vlan, err)
	}
}
