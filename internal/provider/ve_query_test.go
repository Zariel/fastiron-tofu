package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuVEQuery(t *testing.T) {
	var mu sync.Mutex
	native := "ver 09.0.10kT213\ninterface ve 53\n port-name NATIVE NAME\n disable\nend"
	cached, malformed := true, false
	configReads := 0
	server := testswitch.New(t, func(command string) string {
		mu.Lock()
		defer mu.Unlock()
		if command == "show running-config" {
			configReads++
		}
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
		configReads++
		if r.Method != http.MethodGet || r.URL.Path != "/restconf/data/interfaces" {
			t.Errorf("query issued %s %s", r.Method, r.URL.Path)
			w.WriteHeader(405)
			return
		}
		if malformed {
			fmt.Fprint(w, `{}`)
			return
		}
		entry := ""
		if cached {
			entry = `,{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan","description":"CACHED","enabled":true},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`
		}
		fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"}%s]}}`, entry)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(id int) {
		write("main.tf", base+fmt.Sprintf("data \"fastiron_interface_ve\" \"test\" { ve_id = %d }\noutput \"ve\" { value = data.fastiron_interface_ve.test }\n", id))
	}
	choose(53)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "ve")); got != `{"enabled":false,"name":"ve 53","port_name":"NATIVE NAME","ve_id":53,"vlan_id":53}` {
		t.Fatalf("VE query=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	native = "ver 09.0.10kT213\ninterface ve 53\nend"
	cached = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "ve")); got != `{"enabled":true,"name":"ve 53","port_name":"","ve_id":53,"vlan_id":53}` {
		t.Fatalf("native-only default VE=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	native = "ver 09.0.10kT213\nend"
	cached = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "Cannot read VE") {
		t.Fatalf("missing VE error: %s", out)
	}
	mu.Lock()
	native = "ver 09.0.10kT213\ninterface ve 53\nend"
	malformed = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "missing its container") {
		t.Fatalf("missing metadata error: %s", out)
	}

	run(0, "state", "rm", "data.fastiron_interface_ve.test")
	choose(0)
	mu.Lock()
	before := configReads
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "Invalid VE identity") {
		t.Fatalf("missing identity error: %s", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if configReads != before {
		t.Fatal("invalid identity queried VE configuration")
	}
}
