package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuAccessGroupValidation(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for name, tc := range map[string]struct{ family, body, message string }{
		"VE": {"ip", `interface = "ve 5"
acl = "90"`, "canonical Ethernet or LAG"},
		"direction": {"ipv6", `interface = "ethernet 1/1/9"
direction = "ingress"
acl = "V6"`, "direction must be in or out"},
		"MAC egress": {"mac", `interface = "lag 1"
direction = "out"
acl = "MAC"`, "ingress bindings only"},
		"name": {"ip", `interface = "ethernet 1/1/9"
acl = ".."`, "dot-path components"},
	} {
		t.Run(name, func(t *testing.T) {
			write("main.tf", base+"resource \"fastiron_"+tc.family+"_access_group\" \"test\" {\n"+tc.body+"\n}\n")
			if output := run(1, "plan", "-no-color"); !strings.Contains(output, tc.message) {
				t.Fatalf("missing diagnostic %q: %s", tc.message, output)
			}
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writes != 0 {
		t.Fatalf("invalid bindings performed %d writes", s.writes)
	}
}
