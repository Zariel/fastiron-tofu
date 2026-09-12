package voicevlan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestNative(t *testing.T) {
	configuration := "ver 09.0.10k\nauthentication\n voice-vlan 10\ninterface ethernet 1/1/12\n port-name PHONE\n voice-vlan 3053\n disable\n no inline power\ninterface ethernet 1/1/13\n voice-vlan 3054\nend"
	got, err := parse(configuration, "ethernet 1/1/12")
	if err != nil || got.vlanID != 3053 {
		t.Fatalf("state=%+v error=%v", got, err)
	}
	want := "ver 09.0.10k\nauthentication\n voice-vlan 10\n port-name PHONE\n disable\n no inline power\ninterface ethernet 1/1/13\n voice-vlan 3054\nend"
	if strings.Join(got.unowned, "\n") != want {
		t.Fatalf("unowned configuration=%q", got.unowned)
	}
	got, err = parse(configuration, "ethernet 1/1/14")
	if err != nil || got.vlanID != 0 || strings.Join(got.unowned, "\n") != configuration {
		t.Fatalf("default port=%+v error=%v", got, err)
	}

	for _, malformed := range []string{
		strings.TrimSuffix(configuration, "end"),
		strings.Replace(configuration, " voice-vlan 3053", " voice-vlan 3053\n voice-vlan 3054", 1),
		strings.Replace(configuration, " voice-vlan 3053", " voice-vlan 4096", 1),
		strings.Replace(configuration, " voice-vlan 3053", " voice-vlan", 1),
		strings.Replace(configuration, "end", "interface ethernet 1/1/12\nend", 1),
	} {
		if _, err := parse(malformed, "ethernet 1/1/12"); err == nil {
			t.Fatalf("accepted malformed configuration %q", malformed)
		}
	}
}

type switchState struct {
	mu                                          sync.Mutex
	native, cached                              int64
	writes                                      int
	failPatch, corrupt, ignorePatch, omitConfig bool
	portName                                    string
}

func (s *switchState) command(t *testing.T, command string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch command {
	case "skip-page-display":
		return ""
	case "show version":
		return "SW: Version 09.0.10kT213"
	case "show running-config":
		lines := []string{"ver 09.0.10kT213", "authentication", " voice-vlan 10"}
		if s.native != 0 || s.portName != "" {
			lines = append(lines, "interface ethernet 1/1/12")
		}
		if s.portName != "" {
			lines = append(lines, " port-name "+s.portName, " disable", " no inline power")
		}
		if s.native != 0 {
			lines = append(lines, fmt.Sprintf(" voice-vlan %d", s.native))
		}
		lines = append(lines, "interface ethernet 1/1/13", " voice-vlan 3054", "router bgp", " local-as 65011", "end")
		return strings.Join(lines, "\n")
	default:
		t.Errorf("unexpected command %q", command)
		return "% Invalid input"
	}
}

func (s *switchState) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	const config = "/interfaces/interface/ethernet 1/1/12/ethernet/config"
	if r.URL.Path != config && r.URL.Path != config+"/ip-voice-vlan" {
		t.Errorf("unexpected endpoint %s", r.URL.Path)
		w.WriteHeader(404)
		return
	}
	if r.Method == http.MethodGet {
		if s.omitConfig {
			fmt.Fprint(w, `{}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-if-ethernet:config": map[string]any{"icx-openconfig-if-ethernet-aug:ip-voice-vlan": s.cached}})
		return
	}
	s.writes++
	if r.Method == http.MethodDelete && r.URL.Path == config+"/ip-voice-vlan" {
		if s.cached == 0 {
			w.WriteHeader(404)
			return
		}
		s.cached, s.native = 0, 0
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPatch || r.URL.Path != config {
		t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
		w.WriteHeader(405)
		return
	}
	var body map[string]map[string]int64
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
		w.WriteHeader(400)
		return
	}
	fields := body["openconfig-if-ethernet:config"]
	id := fields["ip-voice-vlan"]
	if len(body) != 1 || len(fields) != 1 || id < 1 || id > 4095 {
		t.Error("mutation must contain only a valid voice VLAN")
		w.WriteHeader(400)
		return
	}
	// Firmware skips the native callback when the RESTCONF cache already matches.
	if id != s.cached && !s.ignorePatch {
		s.native = id
	}
	s.cached = id
	if s.corrupt {
		s.portName = "CHANGED"
	}
	if s.failPatch {
		s.failPatch = false
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(204)
}

func TestReconcile(t *testing.T) {
	for _, tc := range []struct {
		name                                                     string
		native, cached, desired                                  int64
		defaultPort, failPatch, corrupt, ignorePatch, omitConfig bool
	}{
		{name: "create", desired: 3053},
		{name: "default port", desired: 3053, defaultPort: true},
		{name: "update", native: 3053, cached: 3053, desired: 3054},
		{name: "drift", native: 3054, cached: 3053, desired: 3053},
		{name: "native only delete", native: 3053},
		{name: "delete", native: 3053, cached: 3053},
		{name: "upper bound", desired: 4095},
		{name: "lower bound", desired: 1},
		{name: "partial failure", desired: 3053, failPatch: true},
		{name: "unrelated change", desired: 3053, corrupt: true},
		{name: "false acknowledgement", desired: 3053, ignorePatch: true},
		{name: "missing container", desired: 3053, omitConfig: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &switchState{native: tc.native, cached: tc.cached, portName: "PHONE", failPatch: tc.failPatch, corrupt: tc.corrupt, ignorePatch: tc.ignorePatch, omitConfig: tc.omitConfig}
			if tc.defaultPort {
				s.portName = ""
			}
			server := testswitch.New(t, func(command string) string { return s.command(t, command) })
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.serve(t, w, r) })
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "manual",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}

			observed, err := apply(context.Background(), device, "ethernet 1/1/12", tc.desired)
			if tc.omitConfig {
				if err == nil || observed != nil || s.writes != 0 {
					t.Fatalf("missing container: observed=%v writes=%d error=%v", observed, s.writes, err)
				}
				return
			}
			if tc.corrupt || tc.ignorePatch {
				want := "unrelated configuration"
				if tc.ignorePatch {
					want = "did not converge"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %s error: %v", want, err)
				}
				return
			}
			if tc.failPatch {
				if err == nil || observed == nil || *observed != 3053 {
					t.Fatalf("partial state=%v error=%v", observed, err)
				}
				observed, err = apply(context.Background(), device, "ethernet 1/1/12", tc.desired)
			}
			if err != nil || observed == nil || *observed != tc.desired {
				t.Fatalf("observed=%v error=%v", observed, err)
			}
			s.mu.Lock()
			native, writes := s.native, s.writes
			s.mu.Unlock()
			if native != tc.desired {
				t.Fatalf("native=%d", native)
			}
			if _, err := apply(context.Background(), device, "ethernet 1/1/12", tc.desired); err != nil {
				t.Fatalf("repeat: %v", err)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.writes != writes {
				t.Fatal("converged operation mutated the switch")
			}
		})
	}
}
