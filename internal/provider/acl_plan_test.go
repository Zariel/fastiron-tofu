package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuACLDriftPlan(t *testing.T) {
	for name, tc := range map[string]struct {
		kind, id, config, native, extra, field string
		want                                   any
	}{
		"MAC source": {
			kind: "mac_access_list", id: "mac access-list EDGE",
			config: `name = "EDGE"
rule {
 action = "permit"
 source = "02:00:00:00:00:01"
}
rule { action = "deny" }`,
			native: " permit 0200.0000.0001 ffff.ffff.ffff any\n deny any any\n",
			extra:  " permit any any ether-type 0806\n", field: "source", want: "02:00:00:00:00:01",
		},
		"extended port": {
			kind: "ip_access_list_extended", id: "ip access-list extended EDGE",
			config: `name = "EDGE"
rule {
 sequence = 10
 action = "permit"
 protocol = 6
 destination_port = "443"
}
rule {
 sequence = 30
 action = "deny"
}`,
			native: " sequence 10 permit tcp any any eq ssl\n sequence 30 deny ip any any\n",
			extra:  " sequence 40 permit icmp any any\n", field: "destination_port", want: "443",
		},
		"IPv6 logging": {
			kind: "ipv6_access_list", id: "ipv6 access-list EDGE",
			config: `name = "EDGE"
rule {
 sequence = 10
 action = "permit"
 protocol = 6
 destination_port = "443"
 log = true
}
rule {
 sequence = 30
 action = "deny"
}`,
			native: " sequence 10 permit tcp any any eq ssl log\n sequence 30 deny ipv6 any any\n",
			extra:  " sequence 40 permit icmp any any\n", field: "log", want: true,
		},
		"standard source": {
			kind: "ip_access_list_standard", id: "ip access-list standard 90",
			config: `name = "90"
rule {
 sequence = 10
 action = "permit"
 source = "192.0.2.0/24"
}
rule {
 sequence = 30
 action = "deny"
}`,
			native: " sequence 10 permit 192.0.2.0 0.0.0.255\n sequence 30 deny any\n",
			extra:  " sequence 40 permit 203.0.113.0 0.0.0.255\n", field: "source", want: "192.0.2.0/24",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			native := "ver 09.0.10kT213\n" + tc.id + "\n" + tc.native + "end"
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					return native
				default:
					return "% Invalid command"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("planning attempted RESTCONF %s %s", r.Method, r.URL.Path)
				http.Error(w, "unexpected request", 500)
			})
			connection := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
			write, run, base := tofuFixture(t, connection)
			address := "fastiron_" + tc.kind + ".test"
			write("main.tf", base+fmt.Sprintf("resource %q \"test\" {\n%s\n}\n", "fastiron_"+tc.kind, tc.config))
			run(0, "init", "-no-color")
			run(0, "import", "-no-color", address, tc.id)

			mu.Lock()
			native = strings.TrimSuffix(native, "end") + tc.extra + "end"
			if tc.kind == "mac_access_list" {
				native = "ver 09.0.10kT213\n" + tc.id + "\n" + tc.extra + tc.native + "end"
			}
			mu.Unlock()
			run(2, "plan", "-detailed-exitcode", "-out=plan.tfplan", "-no-color")
			rules := plannedACLRules(t, run(0, "show", "-json", "plan.tfplan"), address)
			if len(rules) != 2 {
				t.Fatalf("planned rules = %+v", rules)
			}
			found := false
			for index, rule := range rules {
				if (tc.kind == "mac_access_list" && index != 0) || (tc.kind != "mac_access_list" && rule["sequence"] != float64(10)) {
					continue
				}
				found = true
				if rule[tc.field] != tc.want {
					t.Fatalf("removing drift changed configured %s: got %v, want %s", tc.field, rule[tc.field], tc.want)
				}
			}
			if !found {
				t.Fatal("configured sequence 10 missing from plan")
			}
		})
	}
}

func TestOpenTofuACLUnknownMatch(t *testing.T) {
	t.Run("IPv4 source", func(t *testing.T) { testACLUnknown(t, "ip_access_list_extended", "source", `"192.0.2.0/24"`) })
	t.Run("IPv6 logging", func(t *testing.T) { testACLUnknown(t, "ipv6_access_list", "log", "true") })
	t.Run("MAC source", func(t *testing.T) { testACLUnknown(t, "mac_access_list", "source", `"02:00:00:00:00:01"`) })
}

func testACLUnknown(t *testing.T, kind, field, input string) {
	t.Helper()
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	config := fmt.Sprintf(`resource "terraform_data" "prefix" {
 input = %s
}
resource "fastiron_%s" "test" {
 name = "EDGE"
 rule {
  sequence = 10
  action = "permit"
  %s = terraform_data.prefix.output
  protocol = 6
  destination_port = "443"
 }
}
`, input, kind, field)
	if kind == "mac_access_list" {
		config = strings.ReplaceAll(config, "  sequence = 10\n", "")
		config = strings.ReplaceAll(config, "  protocol = 6\n  destination_port = \"443\"\n", "")
	}
	write("main.tf", base+config)
	run(0, "init", "-no-color")
	run(2, "plan", "-detailed-exitcode", "-out=plan.tfplan", "-no-color")
	type change struct {
		Unknown map[string]any `json:"after_unknown"`
	}
	type resourceChange struct {
		Address string `json:"address"`
		Change  change `json:"change"`
	}
	var plan struct {
		Changes []resourceChange `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(run(0, "show", "-json", "plan.tfplan")), &plan); err != nil {
		t.Fatal(err)
	}
	for _, r := range plan.Changes {
		if r.Address != "fastiron_"+kind+".test" {
			continue
		}
		switch unknown := r.Change.Unknown["rule"].(type) {
		case bool:
			if !unknown {
				t.Fatal("unknown rule became known")
			}
		case []any:
			if len(unknown) != 1 || unknown[0].(map[string]any)[field] != true {
				t.Fatalf("unknown %s was defaulted: %+v", field, unknown)
			}
			if kind == "mac_access_list" && field == "source" && unknown[0].(map[string]any)["source_mask"] != true {
				t.Fatalf("MAC mask was defaulted before its address became known: %+v", unknown)
			}
		default:
			t.Fatalf("unknown %s absent from plan: %+v", field, unknown)
		}
		return
	}
	t.Fatal("ACL change missing from plan")
}

func plannedACLRules(t *testing.T, output, address string) []map[string]any {
	t.Helper()
	type values struct {
		Rules []map[string]any `json:"rule"`
	}
	type plannedResource struct {
		Address string `json:"address"`
		Values  values `json:"values"`
	}
	type plannedModule struct {
		Resources []plannedResource `json:"resources"`
	}
	type plannedValues struct {
		Root plannedModule `json:"root_module"`
	}
	var plan struct {
		Values plannedValues `json:"planned_values"`
	}
	if err := json.Unmarshal([]byte(output), &plan); err != nil {
		t.Fatal(err)
	}
	for _, r := range plan.Values.Root.Resources {
		if r.Address == address {
			return r.Values.Rules
		}
	}
	t.Fatalf("%s missing from plan", address)
	return nil
}

func TestOpenTofuACLDefaults(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	anyRule := "rule {\n sequence = 10\n action = \"permit\"\n}\n"
	for name, tc := range map[string]struct{ kind, body string }{
		"IPv4 empty": {"ip_access_list_extended", ""},
		"IPv4 any":   {"ip_access_list_extended", anyRule},
		"IPv6 empty": {"ipv6_access_list", ""},
		"IPv6 any":   {"ipv6_access_list", anyRule},
		"MAC empty":  {"mac_access_list", ""},
		"MAC any":    {"mac_access_list", "rule { action = \"permit\" }\n"},
	} {
		t.Run(name, func(t *testing.T) {
			write("main.tf", base+"resource \"fastiron_"+tc.kind+"\" \"test\" {\n name = \"EDGE\"\n"+tc.body+"}\n")
			run(2, "plan", "-detailed-exitcode", "-out=plan.tfplan", "-no-color")
			rules := plannedACLRules(t, run(0, "show", "-json", "plan.tfplan"), "fastiron_"+tc.kind+".test")
			if tc.body == "" {
				if rules == nil || len(rules) != 0 {
					t.Fatalf("empty ACL rules = %#v", rules)
				}
				return
			}
			if len(rules) != 1 {
				t.Fatalf("defaulted rule = %+v", rules)
			}
			if tc.kind != "ip_access_list_extended" && rules[0]["log"] != false {
				t.Fatalf("logging default = %v", rules[0]["log"])
			}
			fields := []string{"source", "destination", "source_port", "destination_port"}
			if tc.kind == "mac_access_list" {
				fields = []string{"source", "destination", "source_mask", "destination_mask"}
			}
			for _, field := range fields {
				if rules[0][field] != "any" {
					t.Fatalf("default %s = %v", field, rules[0][field])
				}
			}
		})
	}
}
