package lag

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

type interfaceSwitch struct {
	mu                                                                sync.Mutex
	current, cached                                                   interfaceConfig
	disabled, neighbor, saved                                         string
	writes, saves, failAt                                             int
	ignore, corrupt, partialAdmin, nameAdmin, empty, absent, failSave bool
}

func (s *interfaceSwitch) native() string {
	raw := "ver 09.0.10kT213\n"
	if !s.absent {
		raw += "lag TEST static id 5\n"
		if !s.empty {
			raw += " ports ethe 1/1/9 to 1/1/10\n port-name MEMBER ethernet 1/1/9\n"
			if s.disabled != "" {
				raw += " disable ethe " + s.disabled + "\n"
			}
		}
		if !s.empty {
			raw += "interface lag 5\n stp-bpdu-guard\n"
			if s.current.PortName != "" {
				raw += " port-name " + s.current.PortName + "\n"
			}
			if !s.current.Enabled {
				raw += " disable\n"
			}
		}
	} else {
		raw += "interface ethernet 1/1/9\n port-name DETACHED\n disable\n"
	}
	return raw + "interface ethernet 1/1/12\n port-name " + s.neighbor + "\nend"
}

func (s *interfaceSwitch) device(t *testing.T) *fastiron.Device {
	t.Helper()
	s.saved = s.native()
	server := testswitch.New(t, func(command string) string {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return s.native()
		case "show configuration":
			return s.saved
		case "write memory":
			s.saves++
			if s.failSave {
				return "Error: save failed"
			}
			s.saved = s.native()
			return "Write startup-config done."
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/interfaces" {
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9","type":"iana-if-type:ethernetCsmacd"}},{"name":"ethernet 1/1/10","config":{"name":"ethernet 1/1/10","type":"iana-if-type:ethernetCsmacd"}},{"name":"lag 5","config":{"name":"lag 5","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"STATIC","openconfig-if-aggregate-aug:lag-name":"TEST"}}}]}}`)
			return
		}
		s.writes++
		endpoint := "/interfaces/interface=lag 5/config"
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == endpoint:
			var body struct {
				Config struct {
					Description *string `json:"description"`
					Enabled     *bool   `json:"enabled"`
				} `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if value := body.Config.Description; value != nil {
				if *value != s.cached.PortName && !s.ignore {
					s.current.PortName = *value
					if s.nameAdmin {
						s.disabled = ""
					}
				}
				s.cached.PortName = *value
			}
			if value := body.Config.Enabled; value != nil {
				if *value != s.cached.Enabled && !*value && !s.ignore {
					s.current.Enabled = false
					if !s.partialAdmin {
						s.disabled = "1/1/9 to 1/1/10"
					}
				}
				s.cached.Enabled = *value
			}
		case r.Method == http.MethodDelete && r.URL.Path == endpoint+"/description":
			s.cached.PortName = ""
			if !s.ignore {
				s.current.PortName = ""
			}
		case r.Method == http.MethodDelete && r.URL.Path == endpoint+"/enabled":
			s.cached.Enabled = true
			if !s.ignore {
				s.current.Enabled = true
				s.disabled = ""
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(405)
			return
		}
		if s.corrupt {
			s.neighbor = "CHANGED"
		}
		if s.failAt == s.writes {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	})
	device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: 250 * time.Millisecond}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestInterfaceReconcile(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		failAt                                             int
		ignore, corrupt, partialAdmin, nameAdmin, failSave bool
	}{
		{name: "cached desired"},
		{name: "failed priming", failAt: 1},
		{name: "failed mutation", failAt: 2},
		{name: "ignored writes", ignore: true},
		{name: "unrelated change", corrupt: true},
		{name: "incomplete member disable", partialAdmin: true},
		{name: "name changes member admin", nameAdmin: true},
		{name: "failed save", failSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := interfaceConfig{PortName: "DESIRED", Enabled: false}
			if tc.nameAdmin {
				desired.Enabled = true
			}
			sim := &interfaceSwitch{current: interfaceConfig{PortName: "OLD", Enabled: true}, cached: desired, disabled: "1/1/9", neighbor: "NEIGHBOR", failAt: tc.failAt, ignore: tc.ignore, corrupt: tc.corrupt, partialAdmin: tc.partialAdmin, nameAdmin: tc.nameAdmin, failSave: tc.failSave}
			device := sim.device(t)
			saved := sim.saved
			observed, err := applyInterface(context.Background(), device, 5, desired, true)
			failure := tc.failAt != 0 || tc.ignore || tc.corrupt || tc.partialAdmin || tc.nameAdmin || tc.failSave
			if (err != nil) != failure || observed == nil {
				t.Fatalf("observed=%+v error=%v", observed, err)
			}
			sim.mu.Lock()
			if *observed != sim.current {
				t.Errorf("reported=%+v native=%+v", *observed, sim.current)
			}
			if failure && sim.saved != saved {
				t.Error("failed operation changed saved configuration")
			}
			if failure && !tc.failSave && sim.saves != 0 {
				t.Error("failed verification or mutation attempted persistence")
			}
			if tc.failAt != 0 && sim.writes != tc.failAt {
				t.Error("writes continued after a failed request")
			}
			if !failure && (sim.current != desired || !strings.Contains(sim.saved, " port-name DESIRED\n disable\n") || sim.disabled != "1/1/9 to 1/1/10") {
				t.Errorf("native=%+v disabled=%s saved=%s", sim.current, sim.disabled, sim.saved)
			}
			sim.mu.Unlock()
			if failure && !tc.failSave {
				return
			}

			sim.mu.Lock()
			sim.failSave = false
			writes := sim.writes
			sim.mu.Unlock()
			if _, err := applyInterface(context.Background(), device, 5, desired, true); err != nil {
				t.Fatal(err)
			}
			sim.mu.Lock()
			defer sim.mu.Unlock()
			if sim.writes != writes || sim.saved != sim.native() {
				t.Error("retry changed native settings or failed to save")
			}
		})
	}
}

func TestInterfaceDefaults(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		current                interfaceConfig
		disabled               string
		empty, absent, present bool
		desired                interfaceConfig
		failure                bool
	}{
		{name: "name only", current: interfaceConfig{Enabled: true}, disabled: "1/1/9", present: true, desired: interfaceConfig{PortName: "NAME", Enabled: true}},
		{name: "delete", current: interfaceConfig{PortName: "OLD", Enabled: false}, disabled: "1/1/9 to 1/1/10"},
		{name: "absent parent deletion", absent: true},
		{name: "empty defaults", empty: true, current: interfaceConfig{Enabled: true}, present: true, desired: interfaceConfig{Enabled: true}},
		{name: "empty nondefaults", empty: true, current: interfaceConfig{Enabled: true}, present: true, desired: interfaceConfig{PortName: "NAME", Enabled: true}, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sim := &interfaceSwitch{current: tc.current, cached: tc.desired, disabled: tc.disabled, neighbor: "NEIGHBOR", empty: tc.empty, absent: tc.absent}
			device := sim.device(t)
			before := sim.saved
			observed, err := applyInterface(context.Background(), device, 5, tc.desired, tc.present)
			if (err != nil) != tc.failure {
				t.Fatalf("observed=%+v error=%v", observed, err)
			}
			sim.mu.Lock()
			defer sim.mu.Unlock()
			if tc.empty || tc.absent {
				if sim.writes != 0 || sim.saved != before {
					t.Fatal("empty or absent parent received configuration changes")
				}
				return
			}
			desired := tc.desired
			if !tc.present {
				desired = interfaceConfig{Enabled: true}
			}
			if observed == nil || *observed != desired || sim.current != desired {
				t.Fatalf("observed=%+v native=%+v", observed, sim.current)
			}
			if tc.present && sim.disabled != tc.disabled {
				t.Fatal("description update changed independent member administration")
			}
			if !tc.present && sim.disabled != "" {
				t.Fatal("default reset left members disabled")
			}
			if !strings.Contains(sim.saved, " port-name MEMBER ethernet 1/1/9\n") || !strings.Contains(sim.saved, " stp-bpdu-guard\n") {
				t.Fatal("unowned policy was removed")
			}
		})
	}
}
