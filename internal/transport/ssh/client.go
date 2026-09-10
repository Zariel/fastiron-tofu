package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Config struct {
	Address, Username, Password            string
	PrivateKey, KnownHosts, EnablePassword string
	Timeout                                time.Duration
}

type Client struct {
	address        string
	config         *gossh.ClientConfig
	timeout        time.Duration
	enablePassword string
}

func New(cfg Config) (*Client, error) {
	if _, _, err := net.SplitHostPort(cfg.Address); err != nil {
		return nil, errors.New("SSH address must contain a host and port")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("SSH timeout must be positive")
	}
	if strings.TrimSpace(cfg.KnownHosts) == "" {
		return nil, errors.New("SSH known_hosts is required; unknown host keys are not accepted")
	}
	// Use the standard known_hosts implementation, including hashed hosts and
	// revocation markers. The temporary file contains public host keys only.
	f, err := os.CreateTemp("", "fastiron-known-hosts-*")
	if err != nil {
		return nil, errors.New("cannot prepare SSH host-key verification")
	}
	defer os.Remove(f.Name())
	_, writeErr := f.WriteString(cfg.KnownHosts)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return nil, errors.New("cannot prepare SSH host-key verification")
	}
	hostKey, err := knownhosts.New(f.Name())
	if err != nil {
		return nil, errors.New("invalid SSH known_hosts content")
	}
	auth := []gossh.AuthMethod{}
	if cfg.PrivateKey != "" {
		key, err := gossh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			return nil, errors.New("invalid or encrypted SSH private key")
		}
		auth = append(auth, gossh.PublicKeys(key))
	}
	if cfg.Password != "" {
		auth = append(auth, gossh.Password(cfg.Password))
	}
	if len(auth) == 0 {
		return nil, errors.New("SSH requires a password or private key")
	}
	return &Client{address: cfg.Address, timeout: cfg.Timeout, enablePassword: cfg.EnablePassword, config: &gossh.ClientConfig{User: cfg.Username, Auth: auth, HostKeyCallback: hostKey, Timeout: cfg.Timeout}}, nil
}

// Run uses an isolated shell so failed commands cannot leave a later operation
// in the wrong configuration context. Callers serialize complete write workflows.
func (c *Client) Run(ctx context.Context, privileged bool, commands ...string) ([]string, error) {
	for _, command := range commands {
		if !validLine(command) {
			return nil, errors.New("SSH commands must be single printable lines")
		}
	}
	if c.enablePassword != "" && !validLine(c.enablePassword) {
		return nil, errors.New("enable password contains unsupported control characters")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", c.address)
	if err != nil {
		return nil, connectionError(ctx)
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := raw.SetDeadline(deadline); err != nil {
			return nil, connectionError(ctx)
		}
	}
	conn, chans, reqs, err := gossh.NewClientConn(raw, c.address, c.config)
	if err != nil {
		return nil, connectionError(ctx)
	}
	client := gossh.NewClient(conn, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return nil, connectionError(ctx)
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, connectionError(ctx)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, connectionError(ctx)
	}
	if err = session.RequestPty("vt100", 24, 160, gossh.TerminalModes{gossh.ECHO: 0}); err != nil {
		return nil, errors.New("SSH terminal request failed")
	}
	if err = session.Shell(); err != nil {
		return nil, errors.New("SSH shell request failed")
	}
	terminal := newTerminal(stdout, stdin)
	defer terminal.close()
	_, prompt, err := terminal.read(ctx, false)
	if err != nil {
		return nil, err
	}
	if privileged && strings.HasSuffix(prompt, ">") {
		if err = terminal.send("enable"); err != nil {
			return nil, err
		}
		_, prompt, err = terminal.read(ctx, true)
		if err != nil {
			return nil, err
		}
		if prompt == "Password:" {
			if c.enablePassword == "" {
				return nil, errors.New("SSH privilege elevation requires enable_password")
			}
			if err = terminal.send(c.enablePassword); err != nil {
				return nil, err
			}
			_, prompt, err = terminal.read(ctx, false)
			if err != nil {
				return nil, err
			}
		}
		if !strings.HasSuffix(prompt, "#") {
			return nil, errors.New("SSH privilege elevation failed")
		}
	}
	if _, err = terminal.command(ctx, "skip-page-display"); err != nil {
		return nil, fmt.Errorf("disable SSH pagination: %w", err)
	}
	outputs := make([]string, 0, len(commands))
	for i, command := range commands {
		output, err := terminal.command(ctx, command)
		if err != nil {
			return outputs, fmt.Errorf("SSH command %d: %w", i+1, err)
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

func connectionError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("SSH connection failed; check connectivity, credentials, and host-key trust")
}

func validLine(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
