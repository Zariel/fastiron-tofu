package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuEthernetQueries(t *testing.T) {
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		default:
			t.Errorf("query issued unexpected CLI command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/restconf/data/interfaces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("query issued mutation %s", r.Method)
			w.WriteHeader(405)
			return
		}
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[
{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","description":"STALE","enabled":true},"state":{"description":"PHONE","enabled":false,"ifindex":12,"admin-status":"DOWN","oper-status":"DOWN","counters":{"in-octets":"18446744073709551615","out-octets":"9007199254740993","in-errors":"0"}},"openconfig-if-ethernet:ethernet":{"state":{"negotiated-port-speed":"openconfig-if-ethernet:SPEED_UNKNOWN"}}},
{"name":"ethernet 1/1/13","config":{"name":"ethernet 1/1/13","description":"","enabled":true},"state":{"description":"","enabled":true}},
{"name":"ve 1","config":{"name":"ve 1"}}
]}}`)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	configuration := `data "fastiron_interface_ethernet" "phone" { port = "1/1/12" }
data "fastiron_ethernet_interfaces" "all" {}
output "phone" { value = data.fastiron_interface_ethernet.phone }
output "interfaces" { value = data.fastiron_ethernet_interfaces.all.interfaces }
`
	write("main.tf", base+configuration)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")

	var phone struct {
		Name        string                 `json:"name"`
		Port        string                 `json:"port"`
		PortName    string                 `json:"port_name"`
		Enabled     bool                   `json:"enabled"`
		IfIndex     int64                  `json:"ifindex"`
		AdminStatus string                 `json:"admin_status"`
		OperStatus  string                 `json:"oper_status"`
		Counters    map[string]json.Number `json:"counters"`
		Link        struct {
			AutoNegotiate   *bool   `json:"auto_negotiate"`
			Speed           *string `json:"speed"`
			NegotiatedSpeed string  `json:"negotiated_speed"`
		} `json:"link"`
	}
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "phone")), &phone); err != nil {
		t.Fatal(err)
	}
	if phone.Name != "ethernet 1/1/12" || phone.Port != "1/1/12" || phone.PortName != "PHONE" || phone.Enabled || phone.IfIndex != 12 || phone.AdminStatus != "DOWN" || phone.OperStatus != "DOWN" {
		t.Fatalf("incorrect interface state: %+v", phone)
	}
	if phone.Counters["in-octets"].String() != "18446744073709551615" || phone.Counters["out-octets"].String() != "9007199254740993" || phone.Counters["in-errors"].String() != "0" {
		t.Fatalf("counter precision lost through OpenTofu: %+v", phone.Counters)
	}
	if phone.Link.AutoNegotiate != nil || phone.Link.Speed != nil || phone.Link.NegotiatedSpeed != "openconfig-if-ethernet:SPEED_UNKNOWN" {
		t.Fatalf("omitted configuration confused with negotiated state: %+v", phone.Link)
	}
	var interfaces map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "interfaces")), &interfaces); err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 2 || interfaces["ethernet 1/1/12"] == nil || interfaces["ethernet 1/1/13"] == nil {
		t.Fatalf("inventory=%v", interfaces)
	}
	for _, field := range []string{"ifindex", "admin_status", "oper_status", "counters", "link"} {
		if string(interfaces["ethernet 1/1/13"][field]) != "null" {
			t.Fatalf("omitted %s was fabricated: %s", field, interfaces["ethernet 1/1/13"][field])
		}
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	write("main.tf", base+strings.Replace(configuration, `port = "1/1/12"`, `port = "1/1/99"`, 1))
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "Ethernet interface not found") {
		t.Fatalf("missing absent-port diagnostic: %s", output)
	}
	write("main.tf", base+strings.Replace(configuration, `port = "1/1/12"`, `port = "01/1/12"`, 1))
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "Invalid Ethernet identity") {
		t.Fatalf("missing identity diagnostic: %s", output)
	}
}
