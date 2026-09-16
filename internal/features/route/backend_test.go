package route

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestStaticRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		count      int
		failure    bool
	}{
		{"configured", `{"openconfig-network-instance:static-routes":{"static":[{"prefix":"198.18.53.0/24","config":{"prefix":"198.18.53.0/24"},"next-hops":{"next-hop":[{"index":"192.0.2.2","config":{"index":"192.0.2.2","next-hop":"192.0.2.2","metric":200}}]}}]}}`, 1, false},
		{"empty", `{"openconfig-network-instance:static-routes":{}}`, 0, false},
		{"empty hops", `{"openconfig-network-instance:static-routes":{"static":[{"prefix":"198.18.53.0/24","config":{"prefix":"198.18.53.0/24"},"next-hops":{}}]}}`, 0, true},
		{"missing collection", `{}`, 0, true},
		{"null entry", `{"openconfig-network-instance:static-routes":{"static":[null]}}`, 0, true},
		{"missing hops", `{"openconfig-network-instance:static-routes":{"static":[{"prefix":"198.18.53.0/24","config":{"prefix":"198.18.53.0/24"}}]}}`, 0, true},
		{"mismatched gateway", `{"openconfig-network-instance:static-routes":{"static":[{"prefix":"198.18.53.0/24","config":{"prefix":"198.18.53.0/24"},"next-hops":{"next-hop":[{"index":"192.0.2.2","config":{"index":"192.0.2.2","next-hop":"192.0.2.3","metric":200}}]}}]}}`, 0, true},
		{"missing distance", `{"openconfig-network-instance:static-routes":{"static":[{"prefix":"198.18.53.0/24","config":{"prefix":"198.18.53.0/24"},"next-hops":{"next-hop":[{"index":"192.0.2.2","config":{"index":"192.0.2.2","next-hop":"192.0.2.2"}}]}}]}}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			routes, err := cachedRoutes(context.Background(), d)
			if (err != nil) != tc.failure || len(routes) != tc.count {
				t.Fatalf("routes=%v error=%v", routes, err)
			}
			if tc.count == 1 && routes[0] != (route{Prefix: netip.MustParsePrefix("198.18.53.0/24"), NextHop: netip.MustParseAddr("192.0.2.2"), Distance: 200}) {
				t.Fatalf("route=%v", routes[0])
			}
		})
	}
}

func TestStaticProtocolAbsence(t *testing.T) {
	for _, tc := range []struct {
		name, parent string
		failure      bool
	}{
		{"no protocols", `{"openconfig-network-instance:protocols":{}}`, false},
		{"other protocol", `{"openconfig-network-instance:protocols":{"protocol":[{"identifier":"openconfig-policy-types:OSPF","name":"icx-ospf"}]}}`, false},
		{"static exists", `{"openconfig-network-instance:protocols":{"protocol":[{"identifier":"openconfig-policy-types:STATIC","name":"icx-static"}]}}`, true},
		{"missing parent", `{}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/restconf/data/network-instances/network-instance=default-vrf/protocols" {
					fmt.Fprint(w, tc.parent)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			d, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL + "/restconf/data", InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			routes, err := cachedRoutes(context.Background(), d)
			if errors.Is(err, fastiron.ErrNotFound) {
				err = nil
			}
			if len(routes) != 0 || (err != nil) != tc.failure {
				t.Fatalf("routes=%v error=%v", routes, err)
			}
		})
	}
}
