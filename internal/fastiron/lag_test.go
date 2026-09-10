package fastiron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestLAGSynchronization(t *testing.T) {
	const member = `{"name":"ethernet 1/1/7","config":{"name":"ethernet 1/1/7","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 53"}}}`
	const aggregate = `{"name":"lag 53","config":{"name":"lag 53","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"LACP","openconfig-if-aggregate-aug:lag-name":"storage"}}}`
	for _, tc := range []struct {
		name             string
		converges        bool
		operationTimeout time.Duration
		callerTimeout    time.Duration
	}{
		{"converges", true, 5 * time.Second, 5 * time.Second},
		{"caller deadline", false, 5 * time.Second, 100 * time.Millisecond},
		{"operation deadline", false, 100 * time.Millisecond, 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected write %s", r.Method)
				}
				entries := member
				if reads.Add(1) > 1 && tc.converges {
					entries += "," + aggregate
				}
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[%s]}}`, entries)
			}))
			defer server.Close()
			device, err := New(Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: tc.operationTimeout}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), tc.callerTimeout)
			defer cancel()
			got, err := device.LAGs(ctx)
			if !tc.converges {
				if !errors.Is(err, context.DeadlineExceeded) || got != nil {
					t.Fatalf("inconsistent collection reported as success: %#v, %v", got, err)
				}
				if tc.operationTimeout < tc.callerTimeout && ctx.Err() != nil {
					t.Fatal("configured operation timeout did not bound synchronization")
				}
				return
			}
			want := []LAG{{ID: 53, Name: "storage", Mode: "dynamic", Members: []string{"ethernet 1/1/7"}}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("LAGs=%#v error=%v", got, err)
			}
		})
	}
}

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
