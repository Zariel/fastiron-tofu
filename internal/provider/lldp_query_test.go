package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuLLDPQuery(t *testing.T) {
	var mu sync.Mutex
	enabled := true
	body := `{"openconfig-lldp:config":{}}`
	server := testswitch.New(t, func(command string) string {
		mu.Lock()
		defer mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show running-config":
			if enabled {
				return "ver 09.0.10kT213\nend"
			}
			return "ver 09.0.10kT213\nno lldp run\nend"
		case "show version":
			return "SW: Version 09.0.10kT213"
		default:
			t.Errorf("query issued unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("query issued mutation %s", r.Method)
			w.WriteHeader(405)
			return
		}
		switch r.URL.Path {
		case "/restconf/data/lldp/config":
			fmt.Fprint(w, body)
		case "/restconf/data/interfaces":
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[]}}`)
		default:
			t.Errorf("unexpected query %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_lldp" "test" {}
 output "lldp" { value = data.fastiron_lldp.test.enabled }
 `)
	run(0, "init", "-no-color")
	for _, tt := range []struct{ body, want string }{
		{`{"openconfig-lldp:config":{}}`, "true"},
		{`{"openconfig-lldp:config":{"enabled":true}}`, "false"},
		{`{"openconfig-lldp:config":{"enabled":false}}`, "true"},
	} {
		mu.Lock()
		body = tt.body
		enabled = tt.want == "true"
		mu.Unlock()
		run(0, "apply", "-auto-approve", "-no-color")
		if got := strings.TrimSpace(run(0, "output", "-json", "lldp")); got != tt.want {
			t.Fatalf("global LLDP=%s, want %s", got, tt.want)
		}
		run(0, "plan", "-detailed-exitcode", "-no-color")
	}
	mu.Lock()
	body = `{}`
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "missing its configuration container") {
		t.Fatalf("missing container error: %s", out)
	}
}
