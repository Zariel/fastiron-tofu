package ethernet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

type cachedPort struct {
	mu             sync.Mutex
	native, cached config
	writes         int
	failWrite      bool
}

func (s *cachedPort) handle(t *testing.T, w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(map[string]any{"openconfig-interfaces:interfaces": map[string]any{"interface": []any{map[string]any{
			"name":   "ethernet 1/1/12",
			"config": map[string]any{"name": "ethernet 1/1/12", "description": s.cached.PortName, "enabled": s.cached.Enabled},
			"state":  map[string]any{"name": "ethernet 1/1/12", "description": s.native.PortName, "enabled": s.native.Enabled},
		}}}})
		return
	}
	if r.Method != http.MethodPatch {
		t.Errorf("unexpected mutation %s", r.Method)
		w.WriteHeader(405)
		return
	}
	s.writes++
	var body struct {
		Interfaces struct {
			Interface []struct {
				Name   string                     `json:"name"`
				Config map[string]json.RawMessage `json:"config"`
			} `json:"interface"`
		} `json:"interfaces"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
		w.WriteHeader(400)
		return
	}
	if len(body.Interfaces.Interface) != 1 || body.Interfaces.Interface[0].Name != "ethernet 1/1/12" {
		t.Error("wrong mutation target")
		w.WriteHeader(400)
		return
	}
	for key, raw := range body.Interfaces.Interface[0].Config {
		switch key {
		case "name", "type":
		case "description":
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if value != s.cached.PortName {
				s.native.PortName, s.cached.PortName = value, value
			}
		case "enabled":
			var value bool
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if value != s.cached.Enabled {
				s.native.Enabled, s.cached.Enabled = value, value
			}
		default:
			t.Errorf("mutation included unowned field %q", key)
			w.WriteHeader(400)
			return
		}
	}
	if s.failWrite {
		s.failWrite = false
		w.WriteHeader(500)
		fmt.Fprint(w, "mutation interrupted")
		return
	}
	w.WriteHeader(204)
}

func TestCachedConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		native, cached, desired config
		failWrite, partial      bool
	}{
		{name: "description reset", native: config{PortName: "RESTORED"}, cached: config{}, desired: config{}},
		{name: "admin drift", native: config{PortName: "PHONE"}, cached: config{PortName: "PHONE", Enabled: true}, desired: config{PortName: "PHONE", Enabled: true}},
		{name: "both fields", native: config{PortName: "RESTORED"}, cached: config{Enabled: true}, desired: config{Enabled: true}},
		{name: "failed synchronization", native: config{PortName: "RESTORED"}, cached: config{}, desired: config{}, failWrite: true},
		{name: "failed mutation", native: config{}, cached: config{}, desired: config{Enabled: true}, failWrite: true, partial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native, cached, desired := tc.native, tc.cached, tc.desired
			native.Port, cached.Port, desired.Port = "1/1/12", "1/1/12", "1/1/12"
			initial := native
			sim := &cachedPort{native: native, cached: cached, failWrite: tc.failWrite}
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				default:
					t.Errorf("unexpected CLI command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/interfaces", func(w http.ResponseWriter, r *http.Request) { sim.handle(t, w, r) })
			device, err := fastiron.New(fastiron.Config{Host: "switch", Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			current, err := read(context.Background(), device, desired.Port)
			if err != nil || current != initial {
				t.Fatalf("read stale configuration: %+v, %v", current, err)
			}
			observed, err := apply(context.Background(), device, desired)
			if tc.failWrite {
				expected := initial
				if tc.partial {
					expected = desired
				}
				if err == nil || observed == nil || *observed != expected {
					t.Fatalf("failed mutation lost current state: %v, %v", observed, err)
				}
				observed, err = apply(context.Background(), device, desired)
			}
			if err != nil || observed == nil || *observed != desired {
				t.Fatalf("reconciliation=%v, %v", observed, err)
			}
			sim.mu.Lock()
			actual, count := sim.native, sim.writes
			sim.mu.Unlock()
			if actual != desired {
				t.Fatalf("native=%+v", actual)
			}
			if _, err := apply(context.Background(), device, desired); err != nil {
				t.Fatal(err)
			}
			sim.mu.Lock()
			defer sim.mu.Unlock()
			if sim.writes != count {
				t.Fatal("converged operation mutated the port")
			}
		})
	}
}
