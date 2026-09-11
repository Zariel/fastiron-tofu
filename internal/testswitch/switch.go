// Package testswitch serves RESTCONF requests and SSH shell commands for tests.
// Callers own feature state, responses, failure injection, and synchronization.
package testswitch

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Switch accepts REST handlers through ServeMux and commands through the callback
// supplied to New. It makes no assumptions about firmware configuration behavior.
type Switch struct {
	*http.ServeMux
	REST       *httptest.Server
	SSHAddress string
	KnownHosts string
}

// New starts local TLS and SSH servers and registers their cleanup with t.
// Register REST handlers before sending requests. The command callback may be
// called concurrently by separate SSH sessions and must synchronize shared state.
func New(t testing.TB, commandHandler func(string) string) *Switch {
	t.Helper()
	s := &Switch{ServeMux: http.NewServeMux()}
	s.REST = httptest.NewTLSServer(s.ServeMux)
	t.Cleanup(s.REST.Close)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.SSHAddress = listener.Addr().String()
	s.KnownHosts = knownhosts.Line([]string{s.SSHAddress}, signer.PublicKey())
	var connections sync.Map
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	workers.Go(func() {
		defer close(acceptDone)
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(raw, true)
			workers.Go(func() {
				defer connections.Delete(raw)
				defer raw.Close()
				serveConnection(raw, cfg, commandHandler)
			})
		}
	})
	t.Cleanup(func() {
		listener.Close()
		// Stop accepting before closing active sessions so cleanup cannot miss a new connection.
		<-acceptDone
		connections.Range(func(k, v any) bool { k.(net.Conn).Close(); return true })
		workers.Wait()
	})
	return s
}

func serveConnection(raw net.Conn, cfg *ssh.ServerConfig, commandHandler func(string) string) {
	conn, channels, requests, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(requests)

	for request := range channels {
		if request.ChannelType() != "session" {
			request.Reject(ssh.UnknownChannelType, "expected session")
			continue
		}
		channel, requests, err := request.Accept()
		if err != nil {
			return
		}
		serveShell(channel, requests, commandHandler)
		return
	}
}

func serveShell(channel ssh.Channel, requests <-chan *ssh.Request, commandHandler func(string) string) {
	defer channel.Close()
	for request := range requests {
		if request.Type == "pty-req" {
			request.Reply(true, nil)
			continue
		}
		if request.Type != "shell" {
			request.Reply(false, nil)
			continue
		}
		request.Reply(true, nil)
		go ssh.DiscardRequests(requests)
		io.WriteString(channel, "switch#")

		scanner := bufio.NewScanner(channel)
		for scanner.Scan() {
			command := scanner.Text()
			output := commandHandler(command)
			fmt.Fprintf(channel, "%s\r\n%s\r\nswitch#", command, strings.ReplaceAll(output, "\n", "\r\n"))
		}
		return
	}
}
