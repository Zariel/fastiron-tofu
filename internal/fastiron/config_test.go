package fastiron_test

import (
	"context"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestRunningConfig(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
	}{
		{"complete", "Current configuration:\n!\nver 09.0.10k\ndefault-vlan-id 3962\nbanner motd $\nend\n$\nvlan 3962 name DEFAULT-VLAN by port\n!\nend\n", "ver 09.0.10k\ndefault-vlan-id 3962\nbanner motd $\nend\n$\nvlan 3962 name DEFAULT-VLAN by port\nend"},
		{"truncated", "ver 09.0.10k\nvlan 3962 by port", ""},
		{"unrecognized", "not a configuration", ""},
		{"unterminated banner", "ver 09.0.10k\nbanner motd $\nend", ""},
		{"command rejected", "% Invalid input", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show running-config":
					return tc.output
				default:
					t.Errorf("unexpected command: %s", command)
					return "% Invalid input"
				}
			})
			device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "ssh", Persistence: "manual", SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}

			document, err := device.RunningConfig(context.Background())
			if tc.want == "" {
				if err == nil || document != nil {
					t.Fatalf("invalid output returned document=%v error=%v", document, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if document.String() != tc.want {
				t.Fatalf("configuration=%q; want %q", document.String(), tc.want)
			}
			identity, err := document.DefaultVLAN()
			if err != nil || identity != 3962 {
				t.Fatalf("default VLAN=%d error=%v", identity, err)
			}
		})
	}
}
