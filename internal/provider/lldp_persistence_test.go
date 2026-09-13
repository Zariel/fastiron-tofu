package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestOpenTofuLLDPPersistence(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	choose := func(enabled bool) {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_lldp\" \"test\" { enabled = %t }\n", enabled))
	}
	state := func(enabled, pending bool) {
		t.Helper()
		var document struct {
			Values struct {
				Root struct {
					Resources []struct {
						Address string
						Values  map[string]any
					} `json:"resources"`
				} `json:"root_module"`
			} `json:"values"`
		}
		if err := json.Unmarshal([]byte(run(0, "show", "-json")), &document); err != nil {
			t.Fatal(err)
		}
		for _, resource := range document.Values.Root.Resources {
			if resource.Address != "fastiron_lldp.test" {
				continue
			}
			if resource.Values["enabled"] != enabled || resource.Values["persistence_pending"] != pending {
				t.Fatalf("unexpected LLDP state: %+v", resource.Values)
			}
			return
		}
		t.Fatal("LLDP missing from state")
	}
	failSave := func(fail bool) { s.mu.Lock(); s.falseSave = fail; s.mu.Unlock() }
	failed := func(args ...string) {
		t.Helper()
		if output := run(1, args...); !strings.Contains(output, "startup configuration does not match running configuration") {
			t.Fatalf("missing persistence error: %s", output)
		}
	}

	choose(false)
	run(0, "init", "-no-color")
	failSave(true)
	failed("apply", "-auto-approve", "-no-color")
	state(false, true)
	s.mu.Lock()
	if s.lldp || !s.startupLLDP {
		t.Error("failed create did not leave running and saved configuration distinct")
	}
	s.mu.Unlock()
	failSave(false)
	run(0, "apply", "-auto-approve", "-no-color")
	state(false, false)

	choose(true)
	failSave(true)
	failed("apply", "-auto-approve", "-no-color")
	state(true, true)
	s.mu.Lock()
	before := s.writes
	if !s.lldp || s.startupLLDP {
		t.Error("failed update lost native or saved state")
	}
	s.mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	state(true, true)
	failSave(false)
	run(0, "apply", "-auto-approve", "-no-color")
	state(true, false)
	s.mu.Lock()
	if s.writes != before || !s.startupLLDP {
		t.Error("update retry repeated mutation or did not persist")
	}
	s.mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")

	choose(false)
	run(0, "apply", "-auto-approve", "-no-color")
	failSave(true)
	failed("destroy", "-auto-approve", "-no-color")
	s.mu.Lock()
	before = s.writes
	if !s.lldp || s.startupLLDP {
		t.Error("failed delete lost native or saved state")
	}
	s.mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	state(true, true)
	failSave(false)
	run(0, "destroy", "-auto-approve", "-no-color")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writes != before || !s.startupLLDP {
		t.Fatal("delete retry repeated mutation or did not persist")
	}
}
