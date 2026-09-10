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

func TestLAGs(t *testing.T) {
	// Members may precede their aggregate; operational state must not be used
	// to infer configured membership or LACP mode.
	body := `{"openconfig-interfaces:interfaces":{"interface":[
 {"name":"ethernet 1/2/2","config":{"name":"ethernet 1/2/2","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 1"}}},
 {"name":"lag 53","config":{"name":"lag 53","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"STATIC","openconfig-if-aggregate-aug:lag-name":"backup"}}},
 {"name":"lag 1","config":{"name":"lag 1","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"LACP","openconfig-if-aggregate-aug:lag-name":"uplink"},"state":{"lag-type":"STATIC"}}},
 {"name":"ethernet 1/2/1","config":{"name":"ethernet 1/2/1","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 1"}}},
 {"name":"ethernet 1/1/1","config":{"name":"ethernet 1/1/1","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{},"state":{"openconfig-if-aggregate:aggregate-id":"lag 53"}}}
 ]}}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/interfaces" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	device, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := device.LAGs(context.Background())
	want := []LAG{{ID: 1, Name: "uplink", Mode: "dynamic", Members: []string{"ethernet 1/2/1", "ethernet 1/2/2"}}, {ID: 53, Name: "backup", Mode: "static", Members: []string{}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LAGs=%#v error=%v", got, err)
	}
}

func TestLAGCollection(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		failure    bool
	}{
		{"empty", `{"openconfig-interfaces:interfaces":{}}`, false},
		{"unsupported", `{}`, true},
		{"missing configuration", `{"openconfig-interfaces:interfaces":{"interface":[{"name":"lag 1"}]}}`, true},
		{"dangling member", `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1","config":{"name":"ethernet 1/1/1","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 1"}}}]}}`, true},
		{"duplicate identity", `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"},{"name":"ethernet 1/1/1"}]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			device, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = device.LAGs(context.Background())
			if (err != nil) != tc.failure {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
