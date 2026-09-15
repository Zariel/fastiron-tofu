package vlan

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
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
	} {
		t.Run(name, func(t *testing.T) {
			got, err := configtest.Parse(t, tc.config).DefaultVLAN()
			if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
				t.Fatalf("default ID=%d error=%v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestDefaultOwnership(t *testing.T) {
	for _, tc := range []struct {
		operation string
		id        int64
	}{
		{"plan", 1},
		{"apply", 1},
		{"delete", 1},
		{"plan", 3962},
		{"apply", 3962},
		{"delete", 3962},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.operation, tc.id), func(t *testing.T) {
			operation := tc.operation
			native := fmt.Sprintf("ver 09.0.10kT213\ndefault-vlan-id %d\nvlan %d name DEFAULT-VLAN by port\nend", tc.id, tc.id)
			if tc.id == 1 {
				native = strings.Replace(native, "default-vlan-id 1\n", "", 1)
			}
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
					fmt.Fprintf(w, `{"openconfig-network-instance:vlan":[{"vlan-id":%d,"config":{"vlan-id":%d,"name":"DEFAULT-VLAN"}}]}`, tc.id, tc.id)
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
				err = check(ctx, device, Config{ID: tc.id, Name: "ORDINARY"})
			case "apply":
				_, err = apply(ctx, device, Config{ID: tc.id, Name: "ORDINARY"})
			case "delete":
				err = remove(ctx, device, tc.id)
			}
			if operation == "plan" && err != nil {
				t.Fatalf("planning must allow a preceding default VLAN move: %v", err)
			}
			if operation != "plan" && (err == nil || !strings.Contains(err.Error(), "default VLAN")) {
				t.Fatalf("missing default ownership error: %v", err)
			}
			if writes.Load() != 0 {
				t.Fatal("ordinary VLAN operation mutated the default VLAN")
			}
		})
	}
}
