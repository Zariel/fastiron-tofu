package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuLAGInterfaceQuery(t *testing.T) {
	var mu sync.Mutex
	native := "ver 09.0.10kT213\nlag TEST static id 53\n ports ethe 1/1/9 to 1/1/10\n disable ethe 1/1/9 to 1/1/10\ninterface lag 53\n port-name NATIVE NAME\n disable\nend"
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
			entry = `,{"name":"lag 53","config":{"name":"lag 53","type":"iana-if-type:ieee8023adLag","description":"CACHED","enabled":true},"state":{"description":"CACHED","enabled":true}}`
		}
		fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"}%s]}}`, entry)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(id int) {
		write("main.tf", base+fmt.Sprintf("data \"fastiron_interface_lag\" \"test\" { lag_id = %d }\noutput \"lag\" { value = data.fastiron_interface_lag.test }\n", id))
	}
	choose(53)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lag")); got != `{"enabled":false,"lag_id":53,"name":"lag 53","port_name":"NATIVE NAME"}` {
		t.Fatalf("LAG interface query=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	native = "ver 09.0.10kT213\nlag TEST static id 53\n ports ethe 1/1/9 to 1/1/10\n disable ethe 1/1/9\n port-name MEMBER ethernet 1/1/9\nend"
	cached = true
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lag")); got != `{"enabled":true,"lag_id":53,"name":"lag 53","port_name":""}` {
		t.Fatalf("default LAG interface=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	cached = false
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "has not discovered") {
		t.Fatalf("missing parent metadata error: %s", out)
	}

	mu.Lock()
	native = "ver 09.0.10kT213\nend"
	cached = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "Cannot read LAG interface") {
		t.Fatalf("missing LAG error: %s", out)
	}
	mu.Lock()
	native = "ver 09.0.10kT213\nlag TEST static id 53\n ports ethe 1/1/9 to 1/1/10\n disable ethe 1/1/9\n port-name MEMBER ethernet 1/1/9\nend"
	malformed = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "missing or empty") {
		t.Fatalf("missing metadata error: %s", out)
	}

	run(0, "state", "rm", "data.fastiron_interface_lag.test")
	choose(0)
	mu.Lock()
	before := configReads
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "Invalid LAG identity") {
		t.Fatalf("missing identity error: %s", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if configReads != before {
		t.Fatal("invalid identity queried LAG configuration")
	}
}
