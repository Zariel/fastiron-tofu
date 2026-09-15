package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuStormControlQueries(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return "ver 09.0.10kT213\nlag test static id 11\ninterface lag 11\n broadcast limit 111 kbps log\n multicast limit 222 kbps\nend"
		default:
			t.Errorf("unexpected query command %q", command)
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
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}},{"name":"lag 11","config":{"name":"lag 11"}}]}}`)
		case "/restconf/data/openconfig-interfaces:interfaces/interface=ethernet 1/1/12/config/storm_control_config":
			fmt.Fprint(w, `{"icx-openconfig-stormcontrol:storm_control_config":{"broadcast":{"limit":999,"kbps":false}}}`)
		case "/restconf/data/openconfig-interfaces:interfaces/interface=lag 11/config/storm_control_config":
			fmt.Fprint(w, `{"icx-openconfig-stormcontrol:storm_control_config":[null]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_interface_storm_control" "ports" {
 for_each = toset(["ethernet 1/1/12", "lag 11"])
 interface = each.key
}
output "policies" { value = data.fastiron_interface_storm_control.ports }
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	var got map[string]struct {
		Unit      *string `json:"unit"`
		Broadcast *int64  `json:"broadcast_limit"`
		Multicast *int64  `json:"multicast_limit"`
		Unknown   *int64  `json:"unknown_unicast_limit"`
		Options   bool    `json:"has_native_options"`
	}
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "policies")), &got); err != nil {
		t.Fatal(err)
	}
	physical, lag := got["ethernet 1/1/12"], got["lag 11"]
	if len(got) != 2 || physical.Unit != nil || physical.Broadcast != nil || physical.Multicast != nil || physical.Unknown != nil || physical.Options {
		t.Fatalf("default physical policy=%+v", physical)
	}
	if lag.Unit == nil || *lag.Unit != "kbps" || lag.Broadcast == nil || *lag.Broadcast != 111 || lag.Multicast == nil || *lag.Multicast != 222 || lag.Unknown != nil || !lag.Options {
		t.Fatalf("native LAG policy=%+v", lag)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	for _, tc := range []struct{ attributes, diagnostic string }{
		{`unit = "pps"`, "Missing storm limits"},
		{"unit = \"kbps\"\nbroadcast_limit = 1000001", "Invalid storm policy"},
		{"unit = \"mbps\"\nbroadcast_limit = 100", "Invalid storm policy"},
	} {
		write("main.tf", base+"resource \"fastiron_interface_storm_control\" \"test\" {\ninterface = \"ethernet 1/1/12\"\n"+tc.attributes+"\n}\n")
		if out := run(1, "plan", "-no-color"); !strings.Contains(out, tc.diagnostic) {
			t.Fatalf("missing %s: %s", tc.diagnostic, out)
		}
	}
	write("main.tf", base+"resource \"fastiron_interface_storm_control\" \"test\" {\ninterface = \"lag 11\"\nunit = \"kbps\"\nbroadcast_limit = 111\n}\n")
	if out := run(1, "plan", "-no-color"); !strings.Contains(out, "Unsupported storm options") {
		t.Fatalf("missing options diagnostic: %s", out)
	}
	if out := run(1, "import", "-no-color", "fastiron_interface_storm_control.test", "lag 11"); !strings.Contains(out, "Unsupported storm options") {
		t.Fatalf("import adopted unsupported options: %s", out)
	}
}
