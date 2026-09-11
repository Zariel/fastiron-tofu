package authentication

import (
	"context"
	"encoding/json"
	"encoding/pem"
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

type globalSwitch struct {
	mu                     sync.Mutex
	restricted, failure    bool
	failAction, corrupt    bool
	period                 int
	writes, actions, saves int
	startup                string
}

func (s *globalSwitch) native() string {
	text := "ver 09.0.10kT213\nauthentication\n"
	if s.restricted {
		text += " restricted-vlan 3056\n"
	}
	if s.failure {
		text += " auth-fail-action restricted-vlan\n"
	}
	return text + fmt.Sprintf(" reauth-period %d\nend", s.period)
}

func globalDevice(t *testing.T, command func(string) string) (*fastiron.Device, *testswitch.Switch) {
	t.Helper()
	server := testswitch.New(t, command)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.REST.Certificate().Raw})
	device, err := fastiron.New(fastiron.Config{
		Host: server.REST.URL, Transport: "restconf", Persistence: "after_each_write", AllowAAAChanges: true,
		RESTCONF: &restconf.Config{URL: server.REST.URL + "/restconf/data", Username: "automation", Password: "test", CA: string(ca), Timeout: time.Second},
		SSH:      &ssh.Config{Address: server.SSHAddress, Username: "automation", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return device, server
}

func (s *globalSwitch) device(t *testing.T) *fastiron.Device {
	t.Helper()
	s.period = 120
	s.startup = s.native()
	device, server := globalDevice(t, func(command string) string {
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
			return s.startup
		case "write memory":
			s.startup = s.native()
			s.saves++
			return "Write startup-config done."
		default:
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /restconf/data/network-instances/network-instance=default-vrf/vlans/vlan=3056", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":3056,"config":{"vlan-id":3056,"name":"RESTRICTED"}}]}`)
	})
	server.HandleFunc("PATCH /restconf/data/authentication/config", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		var body struct {
			Config map[string]json.RawMessage `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		s.writes++
		for key, value := range body.Config {
			switch key {
			case "restricted-vlan":
				if string(value) != "3056" {
					t.Errorf("restricted VLAN = %s", value)
				}
				s.restricted = true
			case "fail-action":
				if string(value) != `{"fail-action":"restricted-vlan"}` {
					t.Errorf("failure action = %s", value)
				}
				s.failure = true
				s.actions++
			default:
				t.Errorf("unexpected global setting %s", key)
			}
		}
		if s.corrupt {
			s.period = 60
		}
		if s.failAction && s.failure {
			s.failAction = false
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	})
	return device
}

func TestGlobalActionRetry(t *testing.T) {
	s := &globalSwitch{failAction: true}
	device := s.device(t)
	originalStartup := s.startup
	desired := globalConfig{AuthOrder: "dot1x mac-auth", MaxSessions: 2, RestrictedVLAN: 3056, FailureAction: "restricted-vlan"}
	observed, err := applyGlobal(context.Background(), device, desired)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("action failure = %v", err)
	}
	if observed == nil || observed.FailureAction != "restricted-vlan" {
		t.Fatalf("partially applied configuration = %+v", observed)
	}
	if s.saves != 0 || s.startup != originalStartup {
		t.Fatal("failed action was persisted")
	}

	observed, err = applyGlobal(context.Background(), device, desired)
	if err != nil {
		t.Fatal(err)
	}
	if *observed != desired {
		t.Fatalf("configuration = %+v; want %+v", *observed, desired)
	}
	if s.actions != 2 || s.saves != 1 {
		t.Fatalf("action reapplications=%d saves=%d", s.actions, s.saves)
	}
	if s.startup != "ver 09.0.10kT213\nauthentication\n restricted-vlan 3056\n auth-fail-action restricted-vlan\n reauth-period 120\nend" {
		t.Fatalf("startup = %q", s.startup)
	}
}

func TestGlobalNeighborChange(t *testing.T) {
	s := &globalSwitch{corrupt: true}
	device := s.device(t)
	originalStartup := s.startup
	_, err := applyGlobal(context.Background(), device, globalConfig{AuthOrder: "dot1x mac-auth", MaxSessions: 2, RestrictedVLAN: 3056, FailureAction: "restricted-vlan"})
	if err == nil || !strings.Contains(err.Error(), "unrelated native configuration") {
		t.Fatalf("neighbor change = %v", err)
	}
	if s.actions != 0 || s.saves != 0 || s.startup != originalStartup {
		t.Fatal("continued dependent changes or saved after neighbor corruption")
	}
}

func TestGlobalPreflight(t *testing.T) {
	for name, tc := range map[string]struct{ configuration, message string }{
		"enabled port":       {" auth-default-vlan 3055\n dot1x enable\n dot1x enable ethe 1/1/10\n", "disable authentication on ethernet 1/1/10"},
		"failure voice VLAN": {" restricted-vlan 3056\n voice-vlan 3058\n auth-fail-action restricted-vlan voice voice-vlan\n", "unsupported voice VLAN settings"},
		"timeout voice VLAN": {" critical-vlan 3057\n voice-vlan 3058\n auth-timeout-action critical-vlan voice voice-vlan\n", "unsupported voice VLAN settings"},
	} {
		t.Run(name, func(t *testing.T) {
			device, server := globalDevice(t, func(command string) string {
				if command == "show version" {
					return "SW: Version 09.0.10kT213"
				}
				if command == "show running-config" {
					return "ver 09.0.10kT213\nauthentication\n" + tc.configuration + "end"
				}
				return ""
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request before ownership guard: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(500)
			})
			_, err := applyGlobal(context.Background(), device, globalConfig{AuthOrder: "dot1x mac-auth", MaxSessions: 2})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("ownership guard = %v", err)
			}
		})
	}
}
