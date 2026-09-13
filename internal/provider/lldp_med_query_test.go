package provider

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuMEDQuery(t *testing.T) {
	fixture := func(name string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "config", "testdata", "lldp-med", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	var mu sync.Mutex
	native := fixture("voice-middle-untagged.conf")
	cached := fixture("voice-middle-untagged.rest.json")
	inventory, err := os.ReadFile(filepath.Join("..", "config", "testdata", "lldp", "range-disabled.rest.json"))
	if err != nil {
		t.Fatal(err)
	}
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
			t.Errorf("query issued unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("query issued mutation %s", r.Method)
			w.WriteHeader(405)
			return
		}
		switch r.URL.Path {
		case "/restconf/data/interfaces":
			fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[]}}`)
		case "/restconf/data/lldp/med":
			fmt.Fprint(w, cached)
		case "/restconf/data/lldp/interfaces":
			w.Write(inventory)
		default:
			t.Errorf("unexpected query %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_lldp_med_policies" "test" {}
 output "policies" { value = data.fastiron_lldp_med_policies.test.policies }
 `)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	want := `[{"application":"voice","dscp":46,"interface":"ethernet 1/1/10","priority":3,"traffic":"tagged","vlan_id":3053},{"application":"voice","dscp":0,"interface":"ethernet 1/1/11","priority":null,"traffic":"untagged","vlan_id":null},{"application":"voice","dscp":46,"interface":"ethernet 1/1/12","priority":3,"traffic":"tagged","vlan_id":3053}]`
	if got := strings.TrimSpace(run(0, "output", "-json", "policies")); got != want {
		t.Fatalf("native policies=%s; want %s", got, want)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	cached = `{"icx-openconfig-lldp-aug:med":[null]}`
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "policies")); got != want {
		t.Fatalf("cache sentinel hid native policies: %s", got)
	}
	mu.Lock()
	native = "ver 09.0.10k\nend"
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "policies")); got != "[]" {
		t.Fatalf("absent policies=%s", got)
	}

	for _, invalid := range []string{`{}`, `{"icx-openconfig-lldp-aug:med":null}`, `{"icx-openconfig-lldp-aug:med":[]}`} {
		mu.Lock()
		cached = invalid
		mu.Unlock()
		if got := run(1, "plan", "-no-color"); !strings.Contains(got, "Cannot read LLDP-MED policies") {
			t.Fatalf("missing malformed container diagnostic: %s", got)
		}
	}
}
