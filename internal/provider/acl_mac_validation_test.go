package provider

import (
	"fmt"
	"strings"
	"testing"
)

func TestOpenTofuMACName(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for _, name := range []string{"90", "9EDGE", "_EDGE"} {
		t.Run(name, func(t *testing.T) {
			write("main.tf", base+fmt.Sprintf("resource \"fastiron_mac_access_list\" \"test\" { name = %q }", name))
			output := run(1, "plan", "-no-color")
			if !strings.Contains(output, "must begin with an ASCII letter") {
				t.Fatalf("missing MAC name diagnostic: %s", output)
			}
		})
	}
	write("main.tf", base+fmt.Sprintf("resource \"fastiron_mac_access_list\" \"test\" { name = %q }", strings.Repeat("N", 64)))
	run(0, "plan", "-no-color")

	if s.writes != 0 {
		t.Fatal("invalid MAC names reached a mutation")
	}
}
