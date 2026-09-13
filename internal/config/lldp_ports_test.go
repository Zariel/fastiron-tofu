package config

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLLDPCaptures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "lldp", "*.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no captured configurations")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(strings.TrimSuffix(file, ".conf") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				Inventory []string
				Global    *bool
				Ports     map[string]LLDPMode
			}
			if err := json.Unmarshal(data, &expected); err != nil {
				t.Fatal(err)
			}
			if len(expected.Inventory) == 0 || len(expected.Ports) == 0 || expected.Global == nil {
				t.Fatal("fixture is missing its expected results or inventory")
			}
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			document, err := Parse(string(input))
			if err != nil {
				t.Fatal(err)
			}

			modes, _, err := document.LLDPPorts(expected.Inventory)
			if err != nil || !maps.Equal(modes, expected.Ports) {
				t.Fatalf("modes=%+v error=%v; want %+v", modes, err, expected.Ports)
			}
			global, _, err := document.LLDP()
			if err != nil || global != *expected.Global {
				t.Fatalf("global=%t error=%v; want %t", global, err, *expected.Global)
			}
		})
	}
}

func TestLLDPPortScope(t *testing.T) {
	input := "ver 09.0.10k\nno lldp run\nlldp transmit-interval 60\nbanner motd $\nno lldp enable ports all\n$\ninterface ethernet 1/1/10\n no lldp enable ports all\nend"
	document, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	modes, remaining, err := document.LLDPPorts([]string{"ethernet 1/1/10"})
	if err != nil || modes["ethernet 1/1/10"] != (LLDPMode{true, true}) {
		t.Fatalf("unowned command adopted: %+v, %v", modes, err)
	}
	if strings.Join(remaining, "\n") != input {
		t.Fatal("unowned configuration changed")
	}
}

func TestLLDPAllPorts(t *testing.T) {
	document, err := Parse("ver 09.0.10k\nno lldp enable transmit ports all\nend")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]LLDPMode{"ethernet 1/1/1": {true, false}, "ethernet 2/3/4": {true, false}}
	modes, _, err := document.LLDPPorts(slices.Sorted(maps.Keys(want)))
	if err != nil || !maps.Equal(modes, want) {
		t.Fatalf("all ports=%+v error=%v", modes, err)
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
