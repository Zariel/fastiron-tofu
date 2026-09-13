package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuProtectedPortQueries(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return "ver 09.0.10kT213\nlag GUEST static id 11\n ports ethe 1/1/10\ninterface lag 11\n protected-port\nend"
		default:
			t.Errorf("query issued unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("query issued mutation %s", r.Method)
			w.WriteHeader(405)
			return
		}
		switch r.URL.Path {
		case "/restconf/data/interfaces":
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}},{"name":"lag 11","config":{"name":"lag 11"}},{"name":"ethernet 1/1/10","config":{"name":"ethernet 1/1/10"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 11"}}}]}}`)
		case "/restconf/data/protectedport":
			// Cached physical-port protection is stale; native-only LAG protection is omitted.
			fmt.Fprint(w, `{"icx-openconfig-pp:protectedport":{"interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","protectedport":true}}]}}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	configuration := `data "fastiron_interface_protected_port" "ports" {
 for_each = toset(["ethernet 1/1/12", "lag 11"])
 interface = each.key
}
output "protection" { value = { for name, port in data.fastiron_interface_protected_port.ports : name => port.enabled } }
`
	write("main.tf", base+configuration)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	var protection map[string]bool
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "protection")), &protection); err != nil {
		t.Fatal(err)
	}
	if len(protection) != 2 || protection["ethernet 1/1/12"] || !protection["lag 11"] {
		t.Fatalf("native protection=%v", protection)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	for _, name := range []string{"ethernet 1/1/10", "ethernet 1/1/99", "lag 99", "ve 1", "ethernet 01/1/12"} {
		write("main.tf", base+fmt.Sprintf("data \"fastiron_interface_protected_port\" \"test\" { interface = %q }\n", name))
		if output := run(1, "plan", "-no-color"); !strings.Contains(output, "Cannot read interface protected port") {
			t.Fatalf("missing read diagnostic for %s: %s", name, output)
		}
	}

	write("main.tf", base+`resource "fastiron_interface_protected_port" "member" {
 interface = "ethernet 1/1/10"
 enabled = true
}`)
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "on lag 11") {
		t.Fatalf("missing aggregate ownership diagnostic: %s", output)
	}
}
