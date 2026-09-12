package provider

import (
	"fmt"
	"strings"
	"testing"
)

func TestOpenTofuVLANName(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_vlan" "test" {
 vlan_id = 53
 name = "DEFAULT-VLAN"
}
`)
	run(0, "init", "-no-color")
	output := run(1, "plan", "-no-color")
	if !strings.Contains(output, "selects the default VLAN") {
		t.Fatalf("missing ownership diagnostic: %s", output)
	}
	for _, name := range []string{"default-vlan", "Default-Vlan"} {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_vlan\" \"test\" {\n vlan_id = 53\n name = %q\n}\n", name))
		run(0, "plan", "-no-color")
	}
	if s.writes != 0 {
		t.Fatal("reserved name reached a mutation")
	}
}
