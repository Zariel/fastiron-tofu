package provider

import (
	"fmt"
	"strings"
	"testing"
)

func TestOpenTofuDefaultVLANValidation(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for _, id := range []int{0, 4096} {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_default_vlan\" \"test\" { vlan_id = %d }\n", id))
		output := run(1, "plan", "-no-color")
		if !strings.Contains(output, "between 1 and 4095") {
			t.Fatalf("missing range diagnostic for %d: %s", id, output)
		}
	}
	for _, id := range []int{1, 4095} {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_default_vlan\" \"test\" { vlan_id = %d }\n", id))
		run(0, "plan", "-no-color")
	}
	if s.writes != 0 {
		t.Fatal("planning changed switch configuration")
	}
}
