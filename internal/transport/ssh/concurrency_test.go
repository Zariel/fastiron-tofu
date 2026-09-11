package ssh

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestEndpointQueue(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseHold := sync.OnceFunc(func() { close(release) })
	defer releaseHold()
	var secondEntered atomic.Bool
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "hold":
			close(entered)
			<-release
			return "HOLD-OK"
		case "second":
			secondEntered.Store(true)
			return "SECOND-OK"
		default:
			return "% Invalid input"
		}
	})
	cfg := Config{Address: server.SSHAddress, Username: "first", Password: "test", KnownHosts: server.KnownHosts, Timeout: 5 * time.Second}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Username = "second"
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := first.Run(context.Background(), true, "hold")
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("first session did not reach the command: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first session did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := second.Run(ctx, true, "second"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued session error=%v", err)
	}
	if secondEntered.Load() {
		t.Fatal("two clients executed commands concurrently on the same endpoint")
	}

	otherServer := testswitch.New(t, func(command string) string { return "OTHER-OK" })
	cfg.Address, cfg.KnownHosts = otherServer.SSHAddress, otherServer.KnownHosts
	other, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	otherCtx, otherCancel := context.WithTimeout(context.Background(), time.Second)
	defer otherCancel()
	if _, err := other.Run(otherCtx, true, "show version"); err != nil {
		t.Fatalf("a different endpoint was blocked: %v", err)
	}

	releaseHold()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	output, err := second.Run(context.Background(), true, "second")
	if err != nil || len(output) != 1 || output[0] != "SECOND-OK" {
		t.Fatalf("retry after queued cancellation: output=%q error=%v", output, err)
	}
}
