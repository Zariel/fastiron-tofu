package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuSTPDefaultVLAN(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return "ver 09.0.10k\ndefault-vlan-id 4095\nvlan 4095 name DEFAULT-VLAN by port\n spanning-tree\nend"
		default:
			t.Errorf("read/import issued unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/restconf/data/stp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation %s", r.Method)
			w.WriteHeader(405)
			return
		}
		fmt.Fprint(w, `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{"vlan":[{"vlan-id":4095,"config":{"vlan-id":4095,"pvst-priority":32768}}]}}}`)
	})
	server.HandleFunc("/restconf/data/stp/interfaces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation %s", r.Method)
			w.WriteHeader(405)
			return
		}
		fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{}}`)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	config := func(id int) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_spanning_tree_vlan" "test" {
 vlan_id = %d
 mode = "stp"
}
data "fastiron_spanning_tree" "test" {}
output "vlans" { value = data.fastiron_spanning_tree.test.vlans }
`, id))
	}
	config(4095)
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_spanning_tree_vlan.test", "4095")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "vlans")); got != `{"4095":{"mode":"stp","priority":32768}}` {
		t.Fatalf("default VLAN policy=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	for _, id := range []int{0, 4096} {
		config(id)
		if output := run(1, "plan", "-no-color"); !strings.Contains(output, "between 1 and 4095") {
			t.Fatalf("missing range diagnostic for %d: %s", id, output)
		}
	}
}
