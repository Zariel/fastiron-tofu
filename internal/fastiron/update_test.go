package fastiron_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestUpdate(t *testing.T) {
	verificationErr := errors.New("unrelated configuration changed")
	workflowErr := errors.New("desired configuration did not converge")
	for _, tc := range []struct {
		name, method             string
		status                   int
		verification, workflow   error
		noMutation, allowMissing bool
		wantWrites, wantSaves    int
	}{
		{name: "success", status: 204, wantWrites: 2, wantSaves: 1},
		{name: "partial write", status: 500, wantWrites: 1},
		{name: "verification", status: 204, verification: verificationErr, wantWrites: 1},
		{name: "write and verification", status: 500, verification: verificationErr, wantWrites: 1},
		{name: "workflow", status: 204, workflow: workflowErr, wantWrites: 2},
		{name: "no-op persistence retry", status: 204, noMutation: true, wantSaves: 1},
		{name: "missing delete", method: http.MethodDelete, status: 404, wantWrites: 1},
		{name: "accepted missing delete", method: http.MethodDelete, status: 404, allowMissing: true, wantWrites: 2, wantSaves: 1},
		{name: "missing delete with failed verification", method: http.MethodDelete, status: 404, allowMissing: true, verification: verificationErr, wantWrites: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodPatch
			}
			var mu sync.Mutex
			writes, saves, reads := 0, 0, 0
			running, saved := "before", "before"
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "write memory":
					saves++
					saved = running
					return "Write startup-config done."
				case "show running-config":
					return "ver 09.0.10k\nhostname " + running + "\nend"
				case "show configuration":
					return "ver 09.0.10k\nhostname " + saved + "\nend"
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/system/config/hostname", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					reads++
					fmt.Fprintf(w, `{"hostname":%q}`, running)
					return
				}
				if r.Method != method {
					t.Errorf("unexpected method %s", r.Method)
				}
				writes++
				running = "after"
				w.WriteHeader(tc.status)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}
			observed := ""
			verify := func() error {
				var response struct {
					Hostname string `json:"hostname"`
				}
				if err := device.ReadREST(context.Background(), "/system/config/hostname", &response); err != nil {
					return err
				}
				observed = response.Hostname
				return tc.verification
			}

			mutate := func(update *fastiron.Update) error {
				if tc.allowMissing {
					return update.DeleteIfPresent("/system/config/hostname")
				}
				return update.REST(method, "/system/config/hostname", map[string]string{"hostname": "after"})
			}
			var completed *fastiron.Update

			err = device.Update(context.Background(), func(update *fastiron.Update) error {
				completed = update
				if tc.noMutation {
					return tc.workflow
				}
				// Deliberately discard both results: failure must still stop the second
				// request and prevent saving, while leaving readback available to the feature.
				mutate(update)
				if err := verify(); err != nil {
					return err
				}
				mutate(update)
				return tc.workflow
			})
			wantErr := tc.status >= 400 && !tc.allowMissing || tc.verification != nil || tc.workflow != nil
			if (err != nil) != wantErr {
				t.Fatalf("update error = %v, want error %t", err, wantErr)
			}
			if tc.verification != nil && !errors.Is(err, tc.verification) {
				t.Errorf("verification error lost: %v", err)
			}
			if tc.workflow != nil && !errors.Is(err, tc.workflow) {
				t.Errorf("workflow error lost: %v", err)
			}
			if tc.status >= 400 && !tc.allowMissing {
				var statusErr *restconf.HTTPError
				if !errors.As(err, &statusErr) {
					t.Errorf("HTTP error lost: %v", err)
				}
			}
			if err := mutate(completed); err == nil {
				t.Error("completed workflow accepted a mutation")
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != tc.wantWrites || saves != tc.wantSaves || reads != min(1, tc.wantWrites) {
				t.Errorf("writes/reads/saves = %d/%d/%d, want %d/%d/%d", writes, reads, saves, tc.wantWrites, min(1, tc.wantWrites), tc.wantSaves)
			}
			if tc.wantWrites > 0 && observed != "after" {
				t.Errorf("partial state not observed: %q", observed)
			}
			if tc.wantWrites > 0 && tc.wantSaves == 0 && saved != "before" {
				t.Errorf("failed workflow saved %q", saved)
			}
		})
	}
}

func TestUpdateSerialization(t *testing.T) {
	var other *fastiron.Device
	blocked := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		called := false
		err := other.Update(ctx, func(*fastiron.Update) error { called = true; return nil })
		if !errors.Is(err, context.DeadlineExceeded) || called {
			t.Errorf("concurrent workflow acquired device: callback=%t error=%v", called, err)
		}
	}
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "write memory":
			blocked()
			return "Write startup-config done."
		case "show running-config", "show configuration":
			return "ver 09.0.10k\nend"
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	cfg := fastiron.Config{
		Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write",
		RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
		SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
	}
	first, err := fastiron.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Persistence = "manual"
	other, err = fastiron.New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := first.Update(context.Background(), func(*fastiron.Update) error { blocked(); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := other.Update(context.Background(), func(*fastiron.Update) error { return nil }); err != nil {
		t.Fatalf("completed workflow retained device lock: %v", err)
	}
}
