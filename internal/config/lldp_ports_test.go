package config

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestLLDPPorts(t *testing.T) {
	names := []string{"ethernet 1/1/10", "ethernet 1/1/11", "ethernet 1/1/12", "ethernet 1/2/1"}
	for _, tt := range []struct {
		name, commands string
		want           map[string]LLDPMode
	}{
		{"defaults", "", map[string]LLDPMode{"ethernet 1/1/10": {true, true}, "ethernet 1/1/11": {true, true}, "ethernet 1/1/12": {true, true}, "ethernet 1/2/1": {true, true}}},
		{"range", "no lldp enable ports ethe 1/1/10 to 1/1/12\n", map[string]LLDPMode{"ethernet 1/1/10": {false, false}, "ethernet 1/1/11": {false, false}, "ethernet 1/1/12": {false, false}, "ethernet 1/2/1": {true, true}}},
		{"split", "no lldp enable ports ethe 1/1/10 ethe 1/1/12\n", map[string]LLDPMode{"ethernet 1/1/10": {false, false}, "ethernet 1/1/11": {true, true}, "ethernet 1/1/12": {false, false}, "ethernet 1/2/1": {true, true}}},
		{"mixed", "no lldp enable ports ethe 1/1/12\nno lldp enable transmit ports ethe 1/1/10 to 1/1/11\nno lldp enable receive ports ethernet 1/2/1\n", map[string]LLDPMode{"ethernet 1/1/10": {true, false}, "ethernet 1/1/11": {true, false}, "ethernet 1/1/12": {false, false}, "ethernet 1/2/1": {false, true}}},
		{"all", "no lldp enable transmit ports all\n", map[string]LLDPMode{"ethernet 1/1/10": {true, false}, "ethernet 1/1/11": {true, false}, "ethernet 1/1/12": {true, false}, "ethernet 1/2/1": {true, false}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			unowned := "ver 09.0.10k\nno lldp run\nlldp transmit-interval 60\nbanner motd $\nno lldp enable ports all\n$\ninterface ethernet 1/1/10\n no lldp enable ports all\nend"
			input := strings.Replace(unowned, "\ninterface", "\n"+tt.commands+"interface", 1)
			document, err := Parse(input)
			if err != nil {
				t.Fatal(err)
			}
			got, remaining, err := document.LLDPPorts(names)
			if err != nil || !maps.Equal(got, tt.want) {
				t.Fatalf("modes=%+v error=%v", got, err)
			}
			if strings.Join(remaining, "\n") != unowned {
				t.Fatal("unowned configuration changed")
			}
		})
	}
}

func TestLLDPPortCoverage(t *testing.T) {
	for _, command := range []string{
		"no lldp enable ports ethe 1/1/10 to 1/1/12",
		"no lldp enable ports ethe 1/1/11",
		"no lldp enable ports ethe 1/1/10 to 1/1/18446744073709551615",
	} {
		document, err := Parse("ver 09.0.10k\n" + command + "\nend")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := document.LLDPPorts([]string{"ethernet 1/1/10", "ethernet 1/1/12"}); err == nil {
			t.Fatal("accepted inventory missing a configured port")
		}
	}
}

func TestLLDPPortSyntax(t *testing.T) {
	for _, command := range []string{
		"no lldp enable ports", "no lldp enable receive ports ethe", "no lldp enable ports ethe 1/1/10 to",
		"no lldp enable ports ethe 1/1/12 to 1/1/10", "no lldp enable ports ethe 1/1/10 to 1/2/1",
		"no lldp enable ports ethe 01/1/10", "no lldp enable ports ethe 18446744073709551616/1/10",
		"no lldp enable ports ethe 1/1/10 extra", "no lldp enable ports ethe 1/1/10 ethe 1/1/10",
		"no lldp enable ports ethe 1/1/10\nno lldp enable transmit ports ethe 1/1/10",
	} {
		t.Run(command, func(t *testing.T) {
			document, err := Parse("ver 09.0.10k\n" + command + "\nend")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := document.LLDPPorts([]string{"ethernet 1/1/10", "ethernet 1/1/11", "ethernet 1/1/12", "ethernet 1/2/1"}); err == nil {
				t.Fatal("accepted malformed or repeated port setting")
			}
		})
	}
}

func TestLLDPPortRegrouping(t *testing.T) {
	names := []string{"ethernet 1/1/10", "ethernet 1/1/11", "ethernet 1/1/12"}
	before, _ := Parse("ver 09.0.10k\nno lldp enable ports ethe 1/1/10 to 1/1/12\nend")
	after, _ := Parse("ver 09.0.10k\nno lldp enable ports ethe 1/1/10 ethe 1/1/12\nend")
	first, unowned, err := before.LLDPPorts(names)
	if err != nil {
		t.Fatal(err)
	}
	second, remaining, err := after.LLDPPorts(names)
	if err != nil {
		t.Fatal(err)
	}
	if second["ethernet 1/1/11"] != (LLDPMode{true, true}) {
		t.Fatal("middle port was not enabled")
	}
	delete(first, "ethernet 1/1/11")
	delete(second, "ethernet 1/1/11")
	if !maps.Equal(first, second) || !slices.Equal(unowned, remaining) {
		t.Fatal("regrouping changed unowned configuration")
	}
}
