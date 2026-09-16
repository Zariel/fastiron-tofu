package config

import (
	"strings"
	"testing"
)

func TestLAGInterfaceUpdate(t *testing.T) {
	before := "ver 09.0.10kT213\nlag TEST static id 5\n ports ethe 1/1/9 to 1/1/10\n port-name MEMBER ethernet 1/1/9\n disable ethe 1/1/9\nlag OTHER static id 6\n ports ethe 1/1/11\n disable ethe 1/1/11\ninterface lag 5\n port-name OLD\n stp-bpdu-guard\ninterface ethernet 1/1/12\n port-name NEIGHBOR\nend\n"
	renamed := strings.Replace(before, " port-name OLD\n", " port-name NEW\n", 1)
	disabled := strings.Replace(renamed, " disable ethe 1/1/9\n", " disable ethe 1/1/9 to 1/1/10\n", 1)
	disabled = strings.Replace(disabled, " port-name NEW\n", " port-name NEW\n disable\n", 1)
	enabled := strings.Replace(disabled, " disable ethe 1/1/9 to 1/1/10\n", "", 1)
	enabled = strings.Replace(enabled, " disable\n", "", 1)
	for _, tc := range []struct {
		name, before, after string
		invalid             bool
	}{
		{"name preserves member settings", before, renamed, false},
		{"clear name", before, strings.Replace(before, " port-name OLD\n", "", 1), false},
		{"disable all members", before, disabled, false},
		{"enable all members", disabled, enabled, false},
		{"partial disable", before, strings.Replace(disabled, " disable ethe 1/1/9 to 1/1/10\n", " disable ethe 1/1/9\n", 1), true},
		{"partial enable", disabled, strings.Replace(enabled, " ports ethe 1/1/9 to 1/1/10\n", " ports ethe 1/1/9 to 1/1/10\n disable ethe 1/1/9\n", 1), true},
		{"name changes member admin", before, strings.Replace(renamed, " disable ethe 1/1/9\n", "", 1), true},
		{"member name", before, strings.Replace(renamed, "port-name MEMBER", "port-name CHANGED", 1), true},
		{"unrelated interface", before, strings.Replace(renamed, "port-name NEIGHBOR", "port-name CHANGED", 1), true},
		{"unowned virtual policy", before, strings.Replace(renamed, " stp-bpdu-guard\n", "", 1), true},
		{"aggregate name", before, strings.Replace(renamed, "lag TEST", "lag CHANGED", 1), true},
		{"membership", before, strings.Replace(disabled, " ports ethe 1/1/9 to 1/1/10\n", " ports ethe 1/1/9\n", 1), true},
		{"other aggregate admin", before, strings.Replace(disabled, " disable ethe 1/1/11\n", "", 1), true},
		{"nonmember admin", before, strings.Replace(disabled, " disable ethe 1/1/9 to 1/1/10\n", " disable ethe 1/1/9 to 1/1/11\n", 1), true},
		{"malformed admin", before, strings.Replace(disabled, " disable ethe 1/1/9 to 1/1/10\n", " disable future\n", 1), true},
		{"repeated member admin", before, strings.Replace(disabled, " disable ethe 1/1/9 to 1/1/10\n", " disable ethe 1/1/9 to 1/1/10\n disable ethe 1/1/9\n", 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, err := Parse(tc.before)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := Parse(tc.after)
			if err != nil {
				t.Fatal(err)
			}
			err = observed.CheckLAGInterfaceUpdate(original, 5, []string{"ethernet 1/1/9", "ethernet 1/1/10", "ethernet 1/1/11", "ethernet 1/1/12"})
			if (err != nil) != tc.invalid {
				t.Fatalf("preservation error=%v; invalid=%v", err, tc.invalid)
			}
		})
	}
}
