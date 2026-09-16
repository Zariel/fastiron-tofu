package config

import (
	"strings"
	"testing"
)

func TestVE(t *testing.T) {
	for _, tc := range []struct {
		name, body, portName, remaining     string
		exists, children, invalid, disabled bool
	}{
		{name: "absent", body: "interface ve 54\n port-name OTHER\n", remaining: "interface ve 54\n port-name OTHER"},
		{name: "default", body: "interface ve 53\n", exists: true},
		{name: "name", body: "interface ve 53\n port-name Transit  East\n", portName: "Transit  East", exists: true},
		{name: "children", body: "interface ve 53\n port-name EDGE\n disable\n ip address 192.0.2.1/24\n", portName: "EDGE", remaining: " disable\n ip address 192.0.2.1/24", exists: true, children: true, disabled: true},
		{name: "nested name", body: "interface ve 53\n protocol future\n  port-name CHILD\n", remaining: " protocol future\n  port-name CHILD", exists: true, children: true},
		{name: "banner", body: "banner motd #\ninterface ve 53\n port-name FAKE\n#\n", remaining: "banner motd #\ninterface ve 53\n port-name FAKE\n#"},
		{name: "nested disable", body: "interface ve 53\n protocol future\n  disable\n", remaining: " protocol future\n  disable", exists: true, children: true},
		{name: "duplicate disable", body: "interface ve 53\n disable\n disable\n", invalid: true},
		{name: "member disable", body: "interface ve 53\n disable ethe 1/1/7\n", invalid: true},
		{name: "invalid disable", body: "interface ve 53\n disable extra\n", invalid: true},
		{name: "duplicate interface", body: "interface ve 53\ninterface ve 53\n disable\n", invalid: true},
		{name: "duplicate name", body: "interface ve 53\n port-name ONE\n port-name TWO\n", invalid: true},
		{name: "missing name", body: "interface ve 53\n port-name\n", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := Parse("ver 09.0.10kT213\n" + tc.body + "end\n")
			if err != nil {
				t.Fatal(err)
			}
			state, err := document.VE(53)
			if (err != nil) != tc.invalid {
				t.Fatalf("error=%v", err)
			}
			if tc.invalid {
				return
			}

			if state.Exists != tc.exists || state.PortName != tc.portName || state.HasChildren != tc.children || state.Enabled == tc.disabled {
				t.Fatalf("state=%+v", state)
			}
			want := "ver 09.0.10kT213\n"
			if tc.remaining != "" {
				want += tc.remaining + "\n"
			}
			want += "end"
			if got := strings.Join(state.Remaining, "\n"); got != want {
				t.Fatalf("unowned configuration=%q, want %q", got, want)
			}
		})
	}
}
