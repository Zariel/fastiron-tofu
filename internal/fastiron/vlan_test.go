package fastiron

import "testing"

func TestVLANChildren(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		blocked      bool
	}{
		{"empty domain", "ver 09.0.10k\nvlan 53 name INFRA by port\n!\nend", false},
		{"membership", "ver 09.0.10k\nvlan 53 by port\n tagged ethe 1/1/1\n!\nend", true},
		{"routed interface", "ver 09.0.10k\nvlan 53 by port\n!\ninterface ve 53\n!\nend", true},
		{"unrelated VLAN", "ver 09.0.10k\nvlan 54 by port\n tagged ethe 1/1/1\n!\nend", false},
		{"truncated", "ver 09.0.10k\nvlan 53", true},
		{"unrecognized output", "not a configuration", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := vlanChildren(tc.config, 53); (err != nil) != tc.blocked {
				t.Fatalf("child check: %v", err)
			}
		})
	}
}
