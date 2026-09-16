package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuSTPInterfaceInventory(t *testing.T) {
	var cacheCleared atomic.Bool
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return "ver 09.0.10k\ninterface ethernet 1/1/11\n stp-bpdu-guard\ninterface ethernet 1/1/13\n spanning-tree 802-1w admin-edge-port\nend"
		default:
			t.Errorf("query issued command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /restconf/data/stp", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-spanning-tree:stp":{"rapid-pvst":{},"icx-openconfig-spanning-tree-aug:pvst":{}}}`)
	})
	server.HandleFunc("GET /restconf/data/stp/interfaces", func(w http.ResponseWriter, _ *http.Request) {
		if cacheCleared.Load() {
			fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{}}`)
			return
		}
		fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11"}},{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","guard":"ROOT","bpdu-guard":true}}]}}`)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_spanning_tree" "test" {}
output "interfaces" { value = data.fastiron_spanning_tree.test.interfaces }
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	var got map[string]map[string]bool
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "interfaces")), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]bool{
		"ethernet 1/1/11": {"admin_edge": false, "bpdu_guard": true, "root_guard": false},
		"ethernet 1/1/13": {"admin_edge": true, "bpdu_guard": false, "root_guard": false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native interfaces=%v; want %v", got, want)
	}
	cacheCleared.Store(true)
	run(0, "plan", "-detailed-exitcode", "-no-color")
}
