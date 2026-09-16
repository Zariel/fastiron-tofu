package lag

import (
	"strings"
	"testing"
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
