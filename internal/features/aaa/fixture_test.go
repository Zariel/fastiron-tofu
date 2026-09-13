package aaa

import "strings"

// nativeFixture puts command fragments in a complete switch display.
func nativeFixture(commands string) string {
	if strings.HasPrefix(commands, "ver ") {
		return commands
	}
	commands = "ver 09.0.10k\n" + commands
	if !strings.HasSuffix(strings.TrimSpace(commands), "\nend") {
		commands += "\nend"
	}
	return commands
}
