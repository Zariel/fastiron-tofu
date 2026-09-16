package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuLAGInterfaceReplacement(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return "ver 09.0.10k\nlag test static id 11\n ports ethe 1/1/9\ninterface lag 11\n port-name NAME\nend"
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

	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	configure := func(mode, name, trigger string) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_lag" "test" {
 lag_id = 11
 name = %q
 mode = %q
 members = ["ethernet 1/1/9"]
}
resource "fastiron_interface_lag" "test" {
 lag_id = fastiron_lag.test.lag_id
 port_name = "NAME"
 %s
}
`, name, mode, trigger))
	}
	configure("static", "test", "")
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_lag.test", "lag 11")
	run(0, "import", "-no-color", "fastiron_interface_lag.test", "lag 11")

	trigger := "lifecycle { replace_triggered_by = [fastiron_lag.test.id] }"
	for _, tc := range []struct {
		name, mode, lagName, trigger string
		force                        bool
		parent, actions              []string
	}{
		{name: "numeric reference", mode: "dynamic", lagName: "test", parent: []string{"delete", "create"}, actions: []string{"no-op"}},
		{name: "parent identity trigger", mode: "dynamic", lagName: "test", trigger: trigger, parent: []string{"delete", "create"}, actions: []string{"delete", "create"}},
		{name: "rename parent", mode: "static", lagName: "renamed", trigger: trigger, parent: []string{"update"}, actions: []string{"no-op"}},
		{name: "force parent replacement", mode: "static", lagName: "test", trigger: trigger, force: true, parent: []string{"delete", "create"}, actions: []string{"delete", "create"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configure(tc.mode, tc.lagName, tc.trigger)
			args := []string{"plan", "-no-color", "-out=replace.tfplan"}
			if tc.force {
				args = append(args, "-replace=fastiron_lag.test")
			}
			run(0, args...)
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
			if !slices.Equal(actions["fastiron_lag.test"], tc.parent) {
				t.Fatalf("parent actions: %v", actions)
			}
			if !slices.Equal(actions["fastiron_interface_lag.test"], tc.actions) {
				t.Fatalf("interface actions: %v; want %v", actions, tc.actions)
			}
		})
	}
}
