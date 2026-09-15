package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuSTPLAG(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "config", "testdata", "stp-interfaces", "lag-all.conf"))
	if err != nil {
		t.Fatal(err)
	}
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return string(raw)
		default:
			t.Errorf("read/import issued unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /restconf/data/interfaces", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[
   {"name":"lag 11","config":{"name":"lag 11"}},
   {"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 11"}}},
   {"name":"ethernet 1/1/10","config":{"name":"ethernet 1/1/10"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 11"}}}
  ]}}`)
	})
	server.HandleFunc("GET /restconf/data/stp", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{}}}`)
	})
	server.HandleFunc("GET /restconf/data/stp/interfaces", func(w http.ResponseWriter, _ *http.Request) {
		// Inherited member cache entries are not independently configured policies.
		fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9","bpdu-guard":true}}]}}`)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	configure := func(name string) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_spanning_tree_interface" "test" {
 interface = %q
 admin_edge = true
 bpdu_guard = true
 root_guard = true
}
data "fastiron_spanning_tree" "test" { depends_on = [fastiron_spanning_tree_interface.test] }
output "interfaces" { value = data.fastiron_spanning_tree.test.interfaces }
`, name))
	}
	configure("lag 11")
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_spanning_tree_interface.test", "lag 11")
	run(0, "apply", "-auto-approve", "-no-color")
	var got map[string]map[string]bool
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "interfaces")), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]bool{"lag 11": {"admin_edge": true, "bpdu_guard": true, "root_guard": true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native LAG inventory=%v; want %v", got, want)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	for _, name := range []string{"ethernet 1/1/9", "ethernet 1/1/10"} {
		configure(name)
		if output := run(1, "plan", "-no-color"); !strings.Contains(output, "LAG member") {
			t.Fatalf("missing member diagnostic: %s", output)
		}
	}
}
