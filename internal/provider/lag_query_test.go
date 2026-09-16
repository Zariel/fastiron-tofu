package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuLAGQuery(t *testing.T) {
	var mu sync.Mutex
	native := "ver 09.0.10kT213\nlag NATIVE dynamic id 53\n ports ethe 1/1/9 to 1/1/10\nend"
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
			entry = `,{"name":"lag 53","config":{"name":"lag 53","type":"iana-if-type:ieee8023adLag","description":"CACHED","enabled":true},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"STATIC","openconfig-if-aggregate-aug:lag-name":"CACHED"}}}`
		}
		fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/9","config":{"name":"ethernet 1/1/9","type":"iana-if-type:ethernetCsmacd"}},{"name":"ethernet 1/1/10","config":{"name":"ethernet 1/1/10","type":"iana-if-type:ethernetCsmacd"}}%s]}}`, entry)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(id int) {
		write("main.tf", base+fmt.Sprintf("data \"fastiron_lag\" \"test\" { lag_id = %d }\noutput \"lag\" { value = data.fastiron_lag.test }\n", id))
	}
	choose(53)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lag")); got != `{"id":"lag 53","lag_id":53,"members":["ethernet 1/1/10","ethernet 1/1/9"],"mode":"dynamic","name":"NATIVE"}` {
		t.Fatalf("LAG query=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	native = "ver 09.0.10kT213\nlag EMPTY static id 53\nend"
	cached = true
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lag")); got != `{"id":"lag 53","lag_id":53,"members":[],"mode":"static","name":"EMPTY"}` {
		t.Fatalf("empty LAG=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	cached = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lag")); got != `{"id":"lag 53","lag_id":53,"members":[],"mode":"static","name":"EMPTY"}` {
		t.Fatalf("native-only LAG=%s", got)
	}

	mu.Lock()
	native = "ver 09.0.10kT213\nend"
	cached = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "Cannot read LAG") {
		t.Fatalf("missing LAG error: %s", out)
	}
	mu.Lock()
	native = "ver 09.0.10kT213\nlag EMPTY static id 53\nend"
	malformed = true
	mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "missing its container") {
		t.Fatalf("missing metadata error: %s", out)
	}

	run(0, "state", "rm", "data.fastiron_lag.test")
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
