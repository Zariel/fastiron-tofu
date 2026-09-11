package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuGlobalAuthenticationValidation(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for name, tc := range map[string]struct{ body, message string }{
		"zero VLAN":             {`auth_default_vlan = 0`, "VLAN IDs must be between 1 and 4094"},
		"empty action":          {`timeout_action = ""`, "omit an authentication action"},
		"missing default VLAN":  {`dot1x_enabled = true`, "auth_default_vlan is required"},
		"missing critical VLAN": {`timeout_action = "critical-vlan"`, "requires critical_vlan"},
		"invalid order":         {`auth_order = "mac-auth"`, "auth_order must be"},
	} {
		t.Run(name, func(t *testing.T) {
			write("main.tf", base+"resource \"fastiron_authentication\" \"test\" {\n"+tc.body+"\n}\n")
			if output := run(1, "plan", "-no-color"); !strings.Contains(output, tc.message) {
				t.Fatalf("missing diagnostic %q: %s", tc.message, output)
			}
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writes != 0 {
		t.Fatalf("validation performed %d writes", s.writes)
	}
}
