package jumbo

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestRead(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		active   bool
		response string
		wantErr  bool
	}{
		{"native enable", true, false, `{"icx-openconfig-jumbo:jumbo":{"config":{"enabled":false},"operation-state":{"enabled":false}}}`, false},
		{"native disable", false, true, `{"icx-openconfig-jumbo:jumbo":{"config":{"enabled":true},"operation-state":{"enabled":true}}}`, false},
		{"missing value", false, false, `{"icx-openconfig-jumbo:jumbo":{"config":{}}}`, true},
		{"missing container", false, false, `{}`, true},
		{"missing active value", false, false, `{"icx-openconfig-jumbo:jumbo":{"config":{"enabled":false}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testswitch.New(t, func(command string) string {
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					flag := ""
					if tc.enabled {
						flag = "jumbo\n"
					}
					return "ver 09.0.10kT213\n" + flag + "interface ethernet 1/1/12\n port-name EDGE\nend"
				default:
					t.Errorf("unexpected native command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/jumbo", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("query attempted %s", r.Method)
					w.WriteHeader(405)
					return
				}
				fmt.Fprint(w, tc.response)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "after_each_write",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := read(context.Background(), device)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if tc.wantErr {
				return
			}
			if observed.enabled != tc.enabled {
				t.Fatalf("enabled=%v, want %v", observed.enabled, tc.enabled)
			}
			want := "ver 09.0.10kT213\ninterface ethernet 1/1/12\n port-name EDGE\nend"
			if observed.active != tc.active {
				t.Fatalf("active=%v, want %v", observed.active, tc.active)
			}
			if strings.Join(observed.unowned, "\n") != want {
				t.Fatal("unowned configuration changed")
			}
		})
	}
}
