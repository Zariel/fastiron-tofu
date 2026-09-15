package lag

import (
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestLAGName(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{"storage", true},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
		{"", false},
		{"storage\nend", false},
		{"réseau", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(config{ID: 53, Name: tc.name, Mode: "dynamic"})
			if (err == nil) != tc.valid {
				t.Fatalf("name validation: %v; want valid=%v", err, tc.valid)
			}
		})
	}
}

func TestLAGChildren(t *testing.T) {
	lag := config{ID: 53, Name: "test", Mode: "dynamic", Members: []string{"ethernet 1/1/7"}}
	base := "ver 09.0.10k\nlag test dynamic id 53\n ports ethe 1/1/7\n"
	for _, tc := range []struct {
		name, config string
		blocked      bool
	}{
		{"membership", base + "!\nend", false},
		{"member settings", base + " disable ethe 1/1/7\n port-name member ethernet 1/1/7\n!\nend", false},
		{"aggregate setting", base + " trunk-threshold 1\n!\nend", true},
		{"virtual interface", base + "!\nvlan 53 by port\n!\ninterface lag 53\n ip address 192.0.2.1 255.255.255.0\n!\nend", true},
		{"unrelated interface", base + "!\ninterface lag 54\n ip address 192.0.2.2 255.255.255.0\n!\nend", false},
		{"empty virtual interface", base + "!\ninterface lag 53\n!\nend", false},
		{"missing aggregate", "ver 09.0.10k\ninterface lag 53\n!\nend", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := lagChildren(configtest.Parse(t, tc.config), lag); (err != nil) != tc.blocked {
				t.Fatalf("deletion guard: %v; want blocked=%v", err, tc.blocked)
			}
		})
	}
}
