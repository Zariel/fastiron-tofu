package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuSTPSnapshot(t *testing.T) {
	var reads atomic.Uint32
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			// Every read sees one of two complete configurations changed externally.
			if reads.Add(1)%2 == 0 {
				return "ver 09.0.10k\nvlan 53 by port\n spanning-tree priority 202\ninterface ethernet 1/1/12\n no stp-bpdu-guard\nend"
			}
			return "ver 09.0.10k\nvlan 53 by port\n spanning-tree priority 101\ninterface ethernet 1/1/12\n stp-bpdu-guard\nend"
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /restconf/data/stp", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{}}}`)
	})
	server.HandleFunc("GET /restconf/data/stp/interfaces", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{}}`)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_spanning_tree" "test" {}
output "snapshot" {
 value = [data.fastiron_spanning_tree.test.vlans["53"].priority, data.fastiron_spanning_tree.test.interfaces["ethernet 1/1/12"].bpdu_guard]
}
`)

	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	switch got := strings.TrimSpace(run(0, "output", "-json", "snapshot")); got {
	case "[101,true]", "[202,false]":
	default:
		t.Fatalf("query combined different configurations: %s", got)
	}
}
