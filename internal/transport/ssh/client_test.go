package ssh

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func switchServer(t *testing.T) (Config, <-chan string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &gossh.ServerConfig{PasswordCallback: func(c gossh.ConnMetadata, p []byte) (*gossh.Permissions, error) {
		if c.User() == "automation" && string(p) == "secret-marker" {
			return nil, nil
		}
		return nil, errors.New("bad authentication")
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	commands := make(chan string, 40)
	var workers sync.WaitGroup
	var connections sync.Map
	workers.Go(func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(raw, struct{}{})
			workers.Go(func() {
				defer raw.Close()
				defer connections.Delete(raw)
				conn, chans, reqs, err := gossh.NewServerConn(raw, config)
				if err != nil {
					return
				}
				defer conn.Close()
				go gossh.DiscardRequests(reqs)
				for request := range chans {
					channel, reqs, err := request.Accept()
					if err != nil {
						return
					}
					for req := range reqs {
						if req.Type == "pty-req" {
							req.Reply(true, nil)
							continue
						}
						if req.Type != "shell" {
							req.Reply(false, nil)
							continue
						}
						req.Reply(true, nil)
						go gossh.DiscardRequests(reqs)
						io.WriteString(channel, "FastIron\r\nSSH@switch>")
						scanner := bufio.NewScanner(channel)
						for scanner.Scan() {
							line := scanner.Text()
							commands <- line
							switch line {
							case "enable":
								io.WriteString(channel, "Password:")
							case "enable-marker":
								io.WriteString(channel, "\r\nSSH@switch#")
							case "show version":
								io.WriteString(channel, "show version\r\nSW: Version 09.0.10kT213\r\nSSH@switch#")
							case "configure terminal":
								io.WriteString(channel, "configure terminal\r\nSSH@switch(config)#")
							case "show hardware":
								io.WriteString(channel, "show hardware\r\nSerial #")
								time.Sleep(20 * time.Millisecond)
								io.WriteString(channel, ":ABC123\r\nSSH@switch(config)#")
							case "bad command":
								io.WriteString(channel, "bad command\r\n% Invalid input: secret-marker\r\nSSH@switch#")
							case "interface ethernet 1/1/9":
								io.WriteString(channel, "interface ethernet 1/1/9\r\nreceived NULL prompt string for interface ethernet 1/1/9 \r\nAnother configuration is in-progress. Please try again.\r\nSSH@switch(config)#")
							case "stall":
								<-channelDone(channel)
								return
							default:
								fmt.Fprintf(channel, "%s\r\nSSH@switch#", line)
							}
						}
						return
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		listener.Close()
		connections.Range(func(k, v any) bool { k.(net.Conn).Close(); return true })
		workers.Wait()
	})
	return Config{Address: listener.Addr().String(), Username: "automation", Password: "secret-marker", EnablePassword: "enable-marker", KnownHosts: knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey()), Timeout: time.Second}, commands
}

func channelDone(c io.Reader) <-chan struct{} {
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, c); close(done) }()
	return done
}

func TestShell(t *testing.T) {
	cfg, commands := switchServer(t)
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Run(context.Background(), true, "show version")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "SW: Version 09.0.10kT213" {
		t.Fatalf("output: %#v", got)
	}
	// Elevation and pager setup must precede configuration commands on the wire.
	for _, want := range []string{"enable", "enable-marker", "skip-page-display", "show version"} {
		if got := <-commands; got != want {
			t.Fatalf("command %q, want %q", got, want)
		}
	}
}

func TestPromptFraming(t *testing.T) {
	cfg, _ := switchServer(t)
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.Run(context.Background(), true, "configure terminal", "show hardware", "end", "show version")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0] != "" || got[1] != "Serial #:ABC123" || got[2] != "" || got[3] != "SW: Version 09.0.10kT213" {
		t.Fatalf("responses lost their command boundaries: %#v", got)
	}
}

func TestCommandFailure(t *testing.T) {
	cfg, _ := switchServer(t)
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Run(context.Background(), true, "bad command", "write memory")
	if err == nil || strings.Contains(err.Error(), "marker") {
		t.Fatalf("unsafe diagnostic: %v", err)
	}
	_, err = c.Run(context.Background(), true, "show version\nwrite memory")
	if err == nil {
		t.Fatal("accepted command injection")
	}
}

func TestConfigurationBusy(t *testing.T) {
	cfg, commands := switchServer(t)
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Run(context.Background(), true, "configure terminal", "interface ethernet 1/1/9", "port-name changed", "write memory")
	if err == nil {
		t.Fatal("accepted a rejected interface selection")
	}
	for len(commands) > 0 {
		command := <-commands
		if command == "port-name changed" || command == "write memory" {
			t.Fatalf("continued after rejected interface selection: %s", command)
		}
	}
}

func TestCancellation(t *testing.T) {
	cfg, commands := switchServer(t)
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Run(ctx, true, "stall"); done <- err }()
	for command := range commands {
		if command == "stall" {
			break
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close connection")
	}
}

func TestHostKey(t *testing.T) {
	cfg, _ := switchServer(t)
	_, wrongKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongSigner, err := gossh.NewSignerFromKey(wrongKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg.KnownHosts = knownhosts.Line([]string{cfg.Address}, wrongSigner.PublicKey())
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Run(context.Background(), false, "show version"); err == nil {
		t.Fatal("accepted wrong host key")
	}
}
