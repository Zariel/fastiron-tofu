package config

import (
	"strings"
	"testing"
)

func TestJumbo(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		input := "ver 09.0.10k\nbanner motd $\njumbo\n$\ninterface ethernet 1/1/12\n jumbo\nend"
		configured := input
		if enabled {
			configured = strings.Replace(input, "\ninterface", "\njumbo\ninterface", 1)
		}
		document, err := Parse(configured)
		if err != nil {
			t.Fatal(err)
		}
		got, remaining, err := document.Jumbo()
		if err != nil || got != enabled {
			t.Fatalf("enabled=%v error=%v; want %v", got, err, enabled)
		}
		if strings.Join(remaining, "\n") != input {
			t.Fatal("unowned configuration changed")
		}
	}
}

func TestJumboMalformed(t *testing.T) {
	for _, command := range []string{"jumbo extra", "jumbo\njumbo"} {
		document, err := Parse("ver 09.0.10k\n" + command + "\nend")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := document.Jumbo(); err == nil {
			t.Fatalf("accepted %q", command)
		}
	}
}
