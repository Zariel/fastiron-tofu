package fastiron

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestFirmware(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{"7150", "UNIT 1: compiled on Sep 10 labeled as SPR09010k\nSW: Version 09.0.10kT213\nHW: ICX7150-C12P", "09.0.10k"},
		{"7250", "SW: Version 09.0.10kT213\nHW: ICX7250-48", "09.0.10k"},
		{"boot version is not active firmware", "Boot-Monitor Image size = 786944, Version:10.1.18T225", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVersion(tc.output)
			if tc.want == "" {
				if err == nil {
					t.Fatal("accepted unknown firmware")
				}
				return
			}
			if err != nil || got.Firmware != tc.want {
				t.Fatalf("firmware: %+v, %v", got, err)
			}
		})
	}
}

func TestConfiguration(t *testing.T) {
	got, err := configuration("Current configuration:\r\n!\r\nver 09.0.10kT213\r\nvlan 53 name INFRA by port\r\n!\r\nend\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ver 09.0.10kT213\nvlan 53 name INFRA by port\n!\nend" {
		t.Fatalf("configuration: %q", got)
	}
	if _, err = configuration("ver 09.0.10kT213\nvlan 53"); err == nil {
		t.Fatal("accepted truncated output")
	}
}

func TestHostLock(t *testing.T) {
	// Separate configured aliases must serialize even when their credentials and
	// transport clients differ; cancellation must not strand the lock.
	cfg := Config{Host: "SWITCH.EXAMPLE.", Transport: "restconf", Persistence: "never", RESTCONF: &restconf.Config{URL: "https://switch.example/restconf/data", Timeout: time.Second}}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Host = "switch.example"
	cfg.RESTCONF.Username = "another-alias"
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	release, err := a.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = b.lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock: %v", err)
	}
	release()
	release, err = b.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
