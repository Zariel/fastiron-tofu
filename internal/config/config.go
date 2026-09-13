// Package config parses complete FastIron running and startup configurations.
package config

import (
	"errors"
	"strings"
)

//go:generate ragel -Z -o parser.go config.rl
//go:generate gofumpt -w parser.go

type kind uint8

const (
	unknown kind = iota
	trustDSCP
	protectedPort
	voiceVLAN
	stormLimit
	jumboMode
	portName
	adminDisable
)

// Command retains the original command text and its indentation scope. Unknown
// commands remain opaque, so feature ownership never discards unrelated settings.
type Command struct {
	Text   string
	Fields []string
	Parent int
	indent int
	kind   kind
}

type Document struct {
	Commands []Command
	complete bool
	started  bool
	stack    []int
	banner   byte
}

func (d *Document) String() string {
	lines := make([]string, len(d.Commands))
	for i, command := range d.Commands {
		lines[i] = command.Text
	}
	return strings.Join(lines, "\n")
}

func (d *Document) line(raw string) error {
	raw = strings.TrimSuffix(raw, "\r")
	if d.banner != 0 {
		i := len(d.Commands) - 1
		d.Commands[i].Text += "\n" + raw
		if strings.ContainsRune(raw, rune(d.banner)) {
			d.banner = 0
		}
		return nil
	}
	line := strings.TrimRight(raw, " \t")
	if !d.started {
		if !strings.HasPrefix(line, "ver ") {
			return nil
		}
		d.started = true
	}
	fields := commandFields(line)
	if len(fields) == 0 || strings.TrimSpace(line) == "!" {
		return nil
	}
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	for len(d.stack) > 0 && d.Commands[d.stack[len(d.stack)-1]].indent >= indent {
		d.stack = d.stack[:len(d.stack)-1]
	}
	parent := -1
	if len(d.stack) > 0 {
		parent = d.stack[len(d.stack)-1]
	}
	if indent > 0 && parent == -1 {
		return errors.New("native configuration has an orphaned command")
	}
	d.Commands = append(d.Commands, Command{Text: line, Fields: fields, Parent: parent, indent: indent, kind: commandKind(strings.TrimSpace(line))})
	d.stack = append(d.stack, len(d.Commands)-1)
	if indent == 0 && line == "end" {
		d.complete = true
	}
	if indent != 0 || fields[0] != "banner" {
		return nil
	}
	text := strings.TrimLeft(strings.TrimPrefix(raw, "banner"), " \t")
	if len(fields) > 1 && (fields[1] == "motd" || fields[1] == "exec" || fields[1] == "incoming") {
		text = strings.TrimLeft(strings.TrimPrefix(text, fields[1]), " \t")
	}
	if text == "require-enter-key" {
		return nil
	}
	if text == "" || text[0] == '"' {
		return errors.New("native banner has no valid delimiter")
	}
	// Banner whitespace is payload, including trailing spaces on its first line.
	d.Commands[len(d.Commands)-1].Text = raw
	d.banner = text[0]
	if strings.ContainsRune(text[1:], rune(d.banner)) {
		d.banner = 0
	}

	return nil
}

// interfaceHeader returns the unique top-level interface header, or -1 for a default
// interface whose stanza is absent from the configuration.
func (d *Document) interfaceHeader(name string) (int, error) {
	found := -1
	for i, c := range d.Commands {
		if c.Parent != -1 || len(c.Fields) < 2 || c.Fields[0] != "interface" || strings.Join(c.Fields[1:], " ") != name {
			continue
		}
		if found != -1 {
			return -1, errors.New("native configuration repeats the requested interface")
		}
		found = i
	}
	return found, nil
}
