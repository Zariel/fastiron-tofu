package config

import (
	"strings"
	"testing"
)

func TestLLDP(t *testing.T) {
	unowned := "ver 09.0.10k\nbanner motd $\nno lldp run\n$\nno lldp enable ports ethe 1/1/12\nlldp transmit-interval 60\ninterface ethernet 1/1/12\n no lldp run\nend"
	for _, tt := range []struct {
		command string
		enabled bool
	}{
		{"", true}, {"lldp run\n", true}, {"no lldp run\n", false}, {"no\tlldp\trun\n", false},
	} {
		t.Run(tt.command, func(t *testing.T) {
			input := strings.Replace(unowned, "\ninterface", "\n"+tt.command+"interface", 1)
			document, err := Parse(input)
			if err != nil {
				t.Fatal(err)
			}
			enabled, remaining, err := document.LLDP()
			if err != nil || enabled != tt.enabled {
				t.Fatalf("enabled=%t error=%v", enabled, err)
			}
			if strings.Join(remaining, "\n") != unowned {
				t.Fatal("unowned LLDP settings changed")
			}
		})
	}
}

func TestLLDPMalformed(t *testing.T) {
	for _, command := range []string{"lldp run extra", "no lldp run extra", "no lldp run\nno lldp run", "lldp run\nno lldp run"} {
		document, err := Parse("ver 09.0.10k\n" + command + "\nend")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := document.LLDP(); err == nil {
			t.Fatalf("accepted %q", command)
		}
	}
}
