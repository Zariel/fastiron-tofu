package poe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestPoEConfiguration(t *testing.T) {
	// Operational enabled is not a substitute for configured enable state.
	for _, tc := range []struct {
		name, body       string
		enabled, failure bool
	}{
		{"default with stale state", `{"icx-openconfig-if-poe-aug:poe":{"config":{},"state":{"enabled":false}}}`, true, false},
		{"disabled with live state", `{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":false},"state":{"enabled":true}}}`, false, false},
		{"unsupported", `{}`, false, true},
		{"missing configuration", `{"icx-openconfig-if-poe-aug:poe":{"state":{"enabled":true}}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := readPort(context.Background(), device, "ethernet 1/1/12")
			if (err != nil) != tc.failure || p.Enabled != tc.enabled {
				t.Fatalf("PoE=%+v error=%v", p, err)
			}
		})
	}
}

func TestEmptyInterfaceDatabase(t *testing.T) {
	for _, body := range []string{`{"openconfig-interfaces:interfaces":{}}`, `{"openconfig-interfaces:interfaces":{"interface":[]}}`} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		t.Cleanup(server.Close)
		device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readPorts(context.Background(), device); err == nil || errors.Is(err, fastiron.ErrNotFound) {
			t.Fatalf("incomplete database reported as confirmed state: %v", err)
		}
	}
}
