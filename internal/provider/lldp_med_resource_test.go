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

func TestOpenTofuMEDPolicy(t *testing.T) {
	var mu sync.Mutex
	policy := ""
	saved := "ver 09.0.10kT213\nend"
	cache := `{"icx-openconfig-lldp-aug:med":[null]}`
	falseSave := false
	failDelete := false
	writes := 0
	native := func() string { return "ver 09.0.10kT213\n" + policy + "end" }
	server := testswitch.New(t, func(command string) string {
		mu.Lock()
		defer mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return native()
		case "show configuration":
			return saved
		case "write memory":
			if !falseSave {
				saved = native()
			}
			return "Write startup-config done."
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/restconf/data/interfaces":
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[]}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/restconf/data/lldp/interfaces":
			fmt.Fprint(w, `{"openconfig-lldp:interfaces":{"interface":[{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11","enabled":true}}]}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/restconf/data/lldp/interfaces/interface=ethernet 1/1/11":
			fmt.Fprint(w, `{"openconfig-lldp:interface":[{"name":"ethernet 1/1/11","config":{"name":"ethernet 1/1/11","enabled":true}}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/restconf/data/lldp/med":
			fmt.Fprint(w, cache)
		case r.Method == http.MethodDelete:
			if !strings.HasPrefix(r.URL.Path, "/restconf/data/lldp/med/network-policy=voice,") || !strings.HasSuffix(r.URL.Path, "/ports=ethernet 1/1/11") {
				t.Errorf("delete outside ownership: %s", r.URL.Path)
				w.WriteHeader(400)
				return
			}
			writes++
			policy = ""
			cache = `{"icx-openconfig-lldp-aug:med":[null]}`
			if failDelete {
				failDelete = false
				http.Error(w, "injected partial MED deletion", 500)
				return
			}
			w.WriteHeader(204)
		case r.Method == http.MethodPatch && r.URL.Path == "/restconf/data/lldp/med":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			var med struct {
				Policies []map[string]json.RawMessage `json:"network-policy"`
			}
			if err := json.Unmarshal(body["icx-openconfig-lldp-aug:med"], &med); err != nil || len(med.Policies) != 1 {
				t.Errorf("bad policy: %v", err)
				w.WriteHeader(400)
				return
			}
			var app, traffic string
			json.Unmarshal(med.Policies[0]["application"], &app)
			json.Unmarshal(med.Policies[0]["traffic"], &traffic)
			var values []struct {
				VLAN     int64    `json:"vlan"`
				Priority int64    `json:"priority"`
				DSCP     int64    `json:"dscp"`
				Ports    []string `json:"ports"`
			}
			if err := json.Unmarshal(med.Policies[0][traffic], &values); err != nil || len(values) != 1 || app != "voice" || strings.Join(values[0].Ports, ",") != "ethernet 1/1/11" {
				t.Errorf("bad target: %v", err)
				w.WriteHeader(400)
				return
			}
			e := values[0]
			switch traffic {
			case "tagged":
				policy = fmt.Sprintf("lldp med network-policy application voice tagged vlan %d priority %d dscp %d ports ethe 1/1/11\n", e.VLAN, e.Priority, e.DSCP)
			case "priority-tagged":
				policy = fmt.Sprintf("lldp med network-policy application voice priority-tagged priority %d dscp %d ports ethe 1/1/11\n", e.Priority, e.DSCP)
			case "untagged":
				policy = fmt.Sprintf("lldp med network-policy application voice untagged dscp %d ports ethe 1/1/11\n", e.DSCP)
			default:
				t.Errorf("bad traffic %s", traffic)
				w.WriteHeader(400)
				return
			}
			writes++
			raw, _ := json.Marshal(body)
			cache = string(raw)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(fields string) {
		write("main.tf", base+`resource "fastiron_lldp_med_policy" "test" {
 interface = "ethernet 1/1/11"
 application = "voice"
 `+fields+"\n}\n")
	}
	check := func(traffic string, dscp any, pending bool) {
		t.Helper()
		var document struct {
			Values struct {
				Root struct {
					Resources []struct {
						Address string
						Values  map[string]any
					} `json:"resources"`
				} `json:"root_module"`
			} `json:"values"`
		}
		if err := json.Unmarshal([]byte(run(0, "show", "-json")), &document); err != nil {
			t.Fatal(err)
		}
		for _, r := range document.Values.Root.Resources {
			if r.Address != "fastiron_lldp_med_policy.test" {
				continue
			}
			var expectedTraffic any = traffic
			if traffic == "" {
				expectedTraffic = nil
			}
			if r.Values["traffic"] != expectedTraffic || r.Values["dscp"] != dscp || r.Values["persistence_pending"] != pending || r.Values["id"] != "lldp-med|ethernet 1/1/11|voice" {
				t.Fatalf("unexpected MED state: %v", r.Values)
			}
			if traffic == "untagged" && (r.Values["vlan_id"] != nil || r.Values["priority"] != nil) {
				t.Fatalf("untagged retained inapplicable fields: %v", r.Values)
			}
			return
		}
		t.Fatal("MED resource missing from state")
	}
	failSave := func(fail bool) { mu.Lock(); falseSave = fail; mu.Unlock() }
	failed := func(args ...string) {
		t.Helper()
		if out := run(1, args...); !strings.Contains(out, "startup configuration does not match running configuration") {
			t.Fatalf("missing persistence failure: %s", out)
		}
	}
	choose("traffic = \"tagged\"\nvlan_id = 3053\npriority = 3\ndscp = 46")
	run(0, "init", "-no-color")
	failSave(true)
	failed("apply", "-auto-approve", "-no-color")
	check("tagged", float64(46), true)
	failSave(false)
	run(0, "apply", "-auto-approve", "-no-color")
	check("tagged", float64(46), false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	beforeRunning, beforeSaved, beforeWrites := native(), saved, writes
	mu.Unlock()
	run(0, "state", "rm", "fastiron_lldp_med_policy.test")
	run(0, "import", "-no-color", "fastiron_lldp_med_policy.test", "lldp-med|ethernet 1/1/11|voice")
	mu.Lock()
	if native() != beforeRunning || saved != beforeSaved || writes != beforeWrites {
		t.Error("import changed switch configuration")
	}
	mu.Unlock()
	check("tagged", float64(46), false)

	choose("traffic = \"untagged\"\ndscp = 24")
	mu.Lock()
	failDelete = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "RESTCONF returned HTTP 500") {
		t.Fatalf("missing partial failure: %s", out)
	}
	check("", nil, true)
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	check("", nil, true)
	failSave(true)
	failed("apply", "-auto-approve", "-no-color")
	check("untagged", float64(24), true)
	mu.Lock()
	beforeWrites = writes
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	check("untagged", float64(24), true)
	failSave(false)
	run(0, "apply", "-auto-approve", "-no-color")
	check("untagged", float64(24), false)
	mu.Lock()
	if writes != beforeWrites {
		t.Error("update retry repeated completed mutation")
	}
	mu.Unlock()
	choose("traffic = \"untagged\"")
	run(0, "apply", "-auto-approve", "-no-color")
	check("untagged", float64(0), false)
	run(0, "apply", "-auto-approve", "-no-color", "-replace=fastiron_lldp_med_policy.test")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	failSave(true)
	failed("destroy", "-auto-approve", "-no-color")
	check("", nil, true)
	mu.Lock()
	beforeWrites = writes
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	check("", nil, true)
	failSave(false)
	run(0, "destroy", "-auto-approve", "-no-color")
	mu.Lock()
	if writes != beforeWrites || policy != "" || saved != "ver 09.0.10kT213\nend" {
		t.Error("delete retry repeated mutation or failed to persist absence")
	}
	mu.Unlock()

	for _, fields := range []string{"traffic = \"tagged\"", "traffic = \"untagged\"\npriority = 0", "traffic = \"priority-tagged\"\npriority = 3\nvlan_id = 5", "traffic = \"untagged\"\ndscp = 64"} {
		choose(fields)
		run(1, "validate", "-no-color")
	}
}
