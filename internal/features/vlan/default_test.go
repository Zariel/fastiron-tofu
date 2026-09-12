package vlan

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestDefaultID(t *testing.T) {
	for name, tc := range map[string]struct {
		config  string
		want    int64
		wantErr bool
	}{
		"implicit":         {"ver 09.0.10k\nvlan 1 by port\nend", 1, false},
		"configured":       {"ver 09.0.10k\ndefault-vlan-id 3962\nvlan 3962 name DEFAULT-VLAN by port\nend", 3962, false},
		"reserved default": {"ver 09.0.10k\ndefault-vlan-id 4095\nend", 4095, false},
		"missing ID":       {"ver 09.0.10k\ndefault-vlan-id\nend", 0, true},
		"duplicate":        {"ver 09.0.10k\ndefault-vlan-id 3962\ndefault-vlan-id 3963\nend", 0, true},
		"out of range":     {"ver 09.0.10k\ndefault-vlan-id 4096\nend", 0, true},
		"truncated":        {"ver 09.0.10k\ndefault-vlan-id 3962", 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := defaultID(tc.config)
			if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
				t.Fatalf("default ID=%d error=%v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestDefaultOwnership(t *testing.T) {
	for _, operation := range []string{"plan", "apply", "delete"} {
		t.Run(operation, func(t *testing.T) {
			native := "ver 09.0.10kT213\ndefault-vlan-id 3962\nvlan 3962 name DEFAULT-VLAN by port\nend"
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return native
				default:
					t.Errorf("unexpected command: %s", command)
					return "% Invalid input"
				}
			})
			var writes atomic.Int32
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":3962,"config":{"vlan-id":3962,"name":"DEFAULT-VLAN"}}]}`)
					return
				}
				writes.Add(1)
				w.WriteHeader(204)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "manual",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()

			switch operation {
			case "plan":
				err = check(ctx, device, Config{ID: 3962, Name: "ORDINARY"})
			case "apply":
				_, err = apply(ctx, device, Config{ID: 3962, Name: "ORDINARY"})
			case "delete":
				err = remove(ctx, device, 3962)
			}
			if err == nil || !strings.Contains(err.Error(), "default VLAN") {
				t.Fatalf("missing default ownership error: %v", err)
			}
			if writes.Load() != 0 {
				t.Fatal("ordinary VLAN operation mutated the default VLAN")
			}
		})
	}
}
