package ssh

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
)

var (
	promptPattern   = regexp.MustCompile(`(?m)^([A-Za-z0-9_.:/ @-]+(?:\([^\r\n()]*\))?[>#])\s*$`)
	ansiPattern     = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")
	cliErrorPattern = regexp.MustCompile(`(?im)^\s*(?:%\s*(?:error|invalid|unknown|incomplete|ambiguous)|error\s*[:\-]|invalid (?:input|command)|unknown command|incomplete command|ambiguous command|not authorized|permission denied|another configuration is in-progress)`)
)

type chunk struct {
	data string
	err  error
}
type terminal struct {
	input      io.Writer
	chunks     chan chunk
	done       chan struct{}
	promptName string
}

func newTerminal(output io.Reader, input io.Writer) *terminal {
	t := &terminal{input: input, chunks: make(chan chunk), done: make(chan struct{})}
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := output.Read(buffer)
			select {
			case t.chunks <- chunk{string(buffer[:n]), err}:
			case <-t.done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return t
}
func (t *terminal) close() { close(t.done) }
func (t *terminal) send(line string) error {
	if _, err := io.WriteString(t.input, line+"\n"); err != nil {
		return errors.New("SSH terminal write failed")
	}
	return nil
}

func (t *terminal) read(ctx context.Context, password bool) (string, string, error) {
	var buffer strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case c := <-t.chunks:
			buffer.WriteString(c.data)
			if buffer.Len() > 8<<20 {
				return "", "", errors.New("SSH output exceeds size limit")
			}
			output := strings.ReplaceAll(ansiPattern.ReplaceAllString(buffer.String(), ""), "\r", "")
			if password && strings.HasSuffix(strings.TrimSpace(output), "Password:") {
				return "", "Password:", nil
			}
			matches := promptPattern.FindAllStringSubmatchIndex(output, -1)
			if len(matches) > 0 {
				m := matches[len(matches)-1]
				if m[1] == len(output) {
					prompt := strings.TrimSpace(output[m[2]:m[3]])
					name, _, _ := strings.Cut(prompt[:len(prompt)-1], "(")
					// Ordinary output can end a transport chunk at a '#' character.
					// After login, accept only this device's prompt in any CLI mode.
					if t.promptName == "" || name == t.promptName {
						t.promptName = name
						return output[:m[0]], prompt, nil
					}
				}
			}
			if c.err != nil {
				if ctx.Err() != nil {
					return "", "", ctx.Err()
				}
				return "", "", errors.New("SSH connection ended before a complete prompt")
			}
		}
	}
}

func (t *terminal) command(ctx context.Context, command string) (string, error) {
	if err := t.send(command); err != nil {
		return "", err
	}
	output, _, err := t.read(ctx, false)
	if err != nil {
		return "", err
	}
	output = strings.TrimSpace(output)
	if output == command {
		output = ""
	} else {
		output = strings.TrimPrefix(output, command+"\n")
	}
	if cliErrorPattern.MatchString(output) {
		return "", errors.New("FastIron rejected an SSH command; command and response omitted to protect secrets")
	}
	return output, nil
}
