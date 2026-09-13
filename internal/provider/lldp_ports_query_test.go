package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuLLDPPortsQuery(t *testing.T) {
	var mu sync.Mutex
	native := "ver 09.0.10k\nno lldp enable ports ethe 1/1/11\nno lldp enable transmit ports ethe 1/1/10\nno lldp enable receive ports ethe 1/1/12\nend"
	complete := true
	server := testswitch.New(t, func(command string) string {
		mu.Lock()
		defer mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return native
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
		case "/restconf/data/interfaces":
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[]}}`)
		case "/restconf/data/lldp/interfaces/interface=ethernet 1/1/11":
			fmt.Fprint(w, `{"openconfig-lldp:interface":[{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11","enabled":true}}]}`)
		case "/restconf/data/lldp/interfaces":
			entries := []string{}
			for _, name := range []string{"ethernet 1/1/10", "ethernet 1/1/11", "ethernet 1/1/12"} {
				if !complete && name == "ethernet 1/1/11" {
					continue
				}
				entries = append(entries, fmt.Sprintf(`{"name":%q,"config":{"name":%q,"enabled":true}}`, name, name))
			}
			fmt.Fprintf(w, `{"openconfig-lldp:interfaces":{"interface":[%s]}}`, strings.Join(entries, ","))
		default:
			t.Errorf("unexpected query %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_lldp_interface" "test" {
 interface = "ethernet 1/1/11"
 enabled = false
 }
 data "fastiron_lldp_interfaces" "test" {}
 output "lldp" { value = data.fastiron_lldp_interfaces.test.interfaces }
 `)
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_lldp_interface.test", "lldp|ethernet 1/1/11")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lldp")); got != `{"ethernet 1/1/10":true,"ethernet 1/1/11":false,"ethernet 1/1/12":true}` {
		t.Fatalf("native port modes=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	native = "ver 09.0.10k\nno lldp enable ports ethe 1/1/10 to 1/1/12\nend"
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lldp")); got != `{"ethernet 1/1/10":false,"ethernet 1/1/11":false,"ethernet 1/1/12":false}` {
		t.Fatalf("native disabled range=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	complete = false
	mu.Unlock()
	if out := run(1, "plan", "-no-color"); !strings.Contains(out, "omits a port referenced by native configuration") {
		t.Fatalf("missing inventory coverage error: %s", out)
	}
}
