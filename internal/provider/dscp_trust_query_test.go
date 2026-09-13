package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuDSCPTrustQueries(t *testing.T) {
	var sfc atomic.Bool
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			global := ""
			if sfc.Load() {
				global = "symmetrical-flow-control enable\n"
			}
			return "ver 09.0.10kT213\n" + global + "interface ethernet 1/1/12\n trust dscp\nend"
		default:
			t.Errorf("query issued command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("query mutated %s", r.Method)
			w.WriteHeader(405)
			return
		}
		switch r.URL.Path {
		case "/restconf/data/interfaces":
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}},{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11"}},{"name":"lag 11","config":{"name":"lag 11"}},{"name":"ethernet 1/1/10","config":{"name":"ethernet 1/1/10"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 11"}}}]}}`)
		case "/restconf/data/interfaces/interface=ethernet 1/1/12/ethernet/trust-dscp", "/restconf/data/interfaces/interface=ethernet 1/1/11/ethernet/trust-dscp":
			fmt.Fprint(w, `{"icx-openconfig-if-trust-dscp-aug:trust-dscp":{"config":{"enabled":false}}}`)
		case "/restconf/data/interfaces/interface=lag 11/ethernet/trust-dscp":
			w.WriteHeader(404)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	query := `data "fastiron_interface_dscp_trust" "ports" {
 for_each = toset(["ethernet 1/1/12", "ethernet 1/1/11"])
 interface = each.key
}
output "trust" { value = { for name, port in data.fastiron_interface_dscp_trust.ports : name => port.enabled } }
`
	write("main.tf", base+query)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	var got map[string]bool
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "trust")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["ethernet 1/1/12"] || got["ethernet 1/1/11"] {
		t.Fatalf("native trust=%v", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	for _, name := range []string{"lag 11", "ethernet 1/1/10", "ethernet 1/1/99", "ve 1"} {
		write("main.tf", base+fmt.Sprintf("data \"fastiron_interface_dscp_trust\" \"test\" { interface = %q }\n", name))
		if out := run(1, "plan", "-no-color"); !strings.Contains(out, "Cannot read interface DSCP trust") {
			t.Fatalf("missing read diagnostic: %s", out)
		}
	}
	sfc.Store(true)
	write("main.tf", base+query)
	run(0, "apply", "-auto-approve", "-no-color")
	for _, enabled := range []bool{true, false} {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_interface_dscp_trust\" \"test\" {\n interface = \"ethernet 1/1/12\"\n enabled = %t\n}\n", enabled))
		if out := run(1, "plan", "-no-color"); !strings.Contains(out, "Incompatible DSCP trust configuration") {
			t.Fatalf("missing flow-control diagnostic: %s", out)
		}
	}
	if out := run(1, "import", "-no-color", "fastiron_interface_dscp_trust.test", "ethernet 1/1/12"); !strings.Contains(out, "Incompatible DSCP trust configuration") {
		t.Fatalf("import adopted incompatible configuration: %s", out)
	}
}
