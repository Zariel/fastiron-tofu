package igmp

import (
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestGlobalNative(t *testing.T) {
	for _, tc := range []struct {
		name, configuration string
		want                settings
		wantErr             bool
	}{
		{"defaults", "ver 09.0.10k\nvlan 53 by port\n multicast active\n multicast version 3\nend", settings{Mode: "disabled", Version: 2}, false},
		{"overrides", "ver 09.0.10k\nip multicast active\nip multicast query-interval 127\nip multicast version 3\nend", settings{Mode: "active", Version: 3}, false},
		{"default version", "ver 09.0.10k\nip multicast passive\nend", settings{Mode: "passive", Version: 2}, false},
		{"ambiguous mode", "ver 09.0.10k\nip multicast passive\nip multicast active\nend", settings{}, true},
		{"invalid version", "ver 09.0.10k\nip multicast version 1\nend", settings{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGlobal(configtest.Parse(t, tc.configuration))
			if (err != nil) != tc.wantErr || (err == nil && got.settings != tc.want) {
				t.Fatalf("settings=%+v error=%v", got.settings, err)
			}
		})
	}
	got, err := parseGlobal(configtest.Parse(t, "ver 09.0.10k\nip multicast active\nip multicast query-interval 127\nip multicast version 3\nvlan 53 by port\n multicast passive\nend"))
	if err != nil {
		t.Fatal(err)
	}
	want := "ver 09.0.10k\nip multicast query-interval 127\nvlan 53 by port\n multicast passive\nend"
	if strings.Join(got.unowned, "\n") != want {
		t.Fatalf("unowned configuration=%q", got.unowned)
	}
}
