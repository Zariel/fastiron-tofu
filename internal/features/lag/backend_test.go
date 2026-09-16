package lag

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
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
			handler := func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected write %s", r.Method)
				}
				entries := member
				if reads.Add(1) > 1 && tc.converges {
					entries += "," + aggregate
				}
				fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[%s]}}`, entries)
			}
			device := lagDevice(t, "ver 09.0.10k\nlag storage dynamic id 53\n ports ethe 1/1/7\nend", handler, tc.operationTimeout)
			ctx, cancel := context.WithTimeout(context.Background(), tc.callerTimeout)
			defer cancel()
			got, err := readLAGs(ctx, device)
			if !tc.converges {
				if !errors.Is(err, context.DeadlineExceeded) || got != nil {
					t.Fatalf("inconsistent collection reported as success: %#v, %v", got, err)
				}
				if tc.operationTimeout < tc.callerTimeout && ctx.Err() != nil {
					t.Fatal("configured operation timeout did not bound synchronization")
				}
				return
			}
			want := []config{{ID: 53, Name: "storage", Mode: "dynamic", Members: []string{"ethernet 1/1/7"}}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("LAGs=%#v error=%v", got, err)
			}
		})
	}
}

func TestLAGs(t *testing.T) {
	// Native configuration wins over valid but stale cached names, modes and membership.
	body := `{"openconfig-interfaces:interfaces":{"interface":[
 {"name":"ethernet 1/2/2","config":{"name":"ethernet 1/2/2","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 53"}}},
 {"name":"lag 53","config":{"name":"lag 53","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"LACP","openconfig-if-aggregate-aug:lag-name":"old-backup"}}},
 {"name":"lag 1","config":{"name":"lag 1","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"LACP","openconfig-if-aggregate-aug:lag-name":"uplink"},"state":{"lag-type":"STATIC"}}},
 {"name":"ethernet 1/2/1","config":{"name":"ethernet 1/2/1","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 1"}}},
 {"name":"ethernet 1/1/1","config":{"name":"ethernet 1/1/1","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{},"state":{"openconfig-if-aggregate:aggregate-id":"lag 53"}}}
 ]}}`
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/interfaces" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, body)
	}
	device := lagDevice(t, "ver 09.0.10k\nlag uplink dynamic id 1\n ports ethe 1/2/1 to 1/2/2\nlag backup static id 53\nend", handler, time.Second)
	got, err := readLAGs(context.Background(), device)
	want := []config{{ID: 1, Name: "uplink", Mode: "dynamic", Members: []string{"ethernet 1/2/1", "ethernet 1/2/2"}}, {ID: 53, Name: "backup", Mode: "static", Members: []string{}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LAGs=%#v error=%v", got, err)
	}
}

func TestLAGCollection(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		failure    bool
	}{
		{"rebuilding database", `{"openconfig-interfaces:interfaces":{}}`, true},
		{"empty interface list", `{"openconfig-interfaces:interfaces":{"interface":[]}}`, true},
		{"no aggregates", `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1","config":{"name":"ethernet 1/1/1","type":"iana-if-type:ethernetCsmacd"}}]}}`, false},
		{"unsupported", `{}`, true},
		{"missing configuration", `{"openconfig-interfaces:interfaces":{"interface":[{"name":"lag 1"}]}}`, true},
		{"dangling member", `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1","config":{"name":"ethernet 1/1/1","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 1"}}}]}}`, true},
		{"duplicate identity", `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"},{"name":"ethernet 1/1/1"}]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := lagDevice(t, "ver 09.0.10k\nend", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }, time.Second)
			_, err := readLAGs(context.Background(), device)
			if (err != nil) != tc.failure {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func lagDevice(t *testing.T, native string, handler http.HandlerFunc, timeout time.Duration) *fastiron.Device {
	t.Helper()
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return native
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", handler)
	device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: timeout}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestLAGGhost(t *testing.T) {
	device := lagDevice(t, "ver 09.0.10k\nend", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("ghost received mutation %s", r.Method)
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9","type":"iana-if-type:ethernetCsmacd"}},{"name":"lag 11","config":{"name":"lag 11","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"STATIC","openconfig-if-aggregate-aug:lag-name":"test"}}}]}}`)
	}, time.Second)
	observed, err := readLAGs(context.Background(), device)
	if err != nil || len(observed) != 0 {
		t.Fatalf("ghost reported as native LAG: %v, %v", observed, err)
	}
	created, err := applyLAG(context.Background(), device, config{ID: 11, Name: "test", Mode: "static"})
	if err == nil || !strings.Contains(err.Error(), "RESTCONF retains deleted lag 11") || created != nil {
		t.Fatalf("creation=%v error=%v", created, err)
	}
}
