package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuSTPReplacement(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return "ver 09.0.10k\nlag test static id 11\n ports ethe 1/1/9\ninterface lag 11\n stp-bpdu-guard\nend"
		default:
			t.Errorf("planning issued unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /restconf/data/interfaces", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[
 {"name":"lag 11","config":{"name":"lag 11","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"STATIC","openconfig-if-aggregate-aug:lag-name":"test"}}},
 {"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9","type":"iana-if-type:ethernetCsmacd"},"openconfig-if-ethernet:ethernet":{"config":{"openconfig-if-aggregate:aggregate-id":"lag 11"}}}
 ]}}`)
	})
	server.HandleFunc("GET /restconf/data/stp/interfaces", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"lag 11","config":{"name":"lag 11","bpdu-guard":true}}]}}`)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	configure := func(mode, target string) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_lag" "test" {
 lag_id = 11
 name = "test"
 mode = %q
 members = ["ethernet 1/1/9"]
}
resource "fastiron_spanning_tree_interface" "test" {
 interface = %s
 bpdu_guard = true
}
`, mode, target))
	}
	configure("static", "fastiron_lag.test.id")
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_lag.test", "lag 11")
	run(0, "import", "-no-color", "fastiron_spanning_tree_interface.test", "lag 11")

	for _, tc := range []struct {
		name, target string
		actions      []string
	}{
		{"literal identity", `"lag 11"`, []string{"no-op"}},
		{"resource reference", "fastiron_lag.test.id", []string{"delete", "create"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configure("dynamic", tc.target)
			run(0, "plan", "-no-color", "-out=replace.tfplan")
			var plan struct {
				Changes []struct {
					Address string `json:"address"`
					Change  struct {
						Actions []string `json:"actions"`
					} `json:"change"`
				} `json:"resource_changes"`
			}
			if err := json.Unmarshal([]byte(run(0, "show", "-json", "replace.tfplan")), &plan); err != nil {
				t.Fatal(err)
			}
			actions := map[string][]string{}
			for _, change := range plan.Changes {
				actions[change.Address] = change.Change.Actions
			}
			if !slices.Equal(actions["fastiron_lag.test"], []string{"delete", "create"}) {
				t.Fatalf("parent actions: %v", actions)
			}
			if !slices.Equal(actions["fastiron_spanning_tree_interface.test"], tc.actions) {
				t.Fatalf("STP actions: %v; want %v", actions, tc.actions)
			}
		})
	}
}
