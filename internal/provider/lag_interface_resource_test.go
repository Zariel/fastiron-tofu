package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuLAGInterface(t *testing.T) {
	type policy struct {
		name    string
		enabled bool
	}
	var mu sync.Mutex
	current := map[int]policy{53: {enabled: true}, 54: {enabled: true}}
	parents := map[int]bool{53: true, 54: true}
	writes, saves := 0, 0
	failSave := true
	native := func() string {
		var b strings.Builder
		b.WriteString("ver 09.0.10kT213\n")
		for _, id := range []int{53, 54} {
			first := 9 + (id-53)*2
			if !parents[id] {
				for _, port := range []int{first, first + 1} {
					fmt.Fprintf(&b, "interface ethernet 1/1/%d\n port-name DETACHED\n disable\n", port)
				}
				continue
			}
			value := current[id]
			fmt.Fprintf(&b, "lag LAG%d static id %d\n ports ethe 1/1/%d to 1/1/%d\n port-name MEMBER%d ethernet 1/1/%d\n", id, id, first, first+1, id, first)
			if !value.enabled {
				fmt.Fprintf(&b, " disable ethe 1/1/%d to 1/1/%d\n", first, first+1)
			}
			fmt.Fprintf(&b, "interface lag %d\n stp-bpdu-guard\n", id)
			if value.name != "" {
				fmt.Fprintf(&b, " port-name %s\n", value.name)
			}
			if !value.enabled {
				b.WriteString(" disable\n")
			}
		}
		b.WriteString("interface ethernet 1/1/1\n port-name NEIGHBOR\nend")
		return b.String()
	}
	saved := native()
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
			saves++
			if failSave {
				return "% Error saving configuration"
			}
			saved = native()
			return "Write startup-config done."
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/restconf/data/interfaces" {
			entries := []any{}
			for _, port := range []int{9, 10, 11, 12} {
				name := fmt.Sprintf("ethernet 1/1/%d", port)
				entries = append(entries, map[string]any{"name": name, "config": map[string]any{"name": name, "type": "iana-if-type:ethernetCsmacd"}})
			}
			for _, id := range []int{53, 54} {
				name := fmt.Sprintf("lag %d", id)
				entries = append(entries, map[string]any{"name": name, "config": map[string]any{"name": name, "type": "iana-if-type:ieee8023adLag", "description": "CACHED", "enabled": true}, "openconfig-if-aggregate:aggregation": map[string]any{"config": map[string]any{"lag-type": "STATIC", "openconfig-if-aggregate-aug:lag-name": fmt.Sprintf("LAG%d", id)}}})
			}
			json.NewEncoder(w).Encode(map[string]any{"openconfig-interfaces:interfaces": map[string]any{"interface": entries}})
			return
		}
		suffix, ok := strings.CutPrefix(r.URL.Path, "/restconf/data/interfaces/interface=lag ")
		idText, leaf, valid := strings.Cut(suffix, "/config")
		id, err := strconv.Atoi(idText)
		if !ok || !valid || err != nil || !parents[id] {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		writes++
		value := current[id]
		switch {
		case r.Method == http.MethodPatch && leaf == "":
			var body struct {
				Config struct {
					Description *string `json:"description"`
					Enabled     *bool   `json:"enabled"`
				} `json:"config"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if body.Config.Description != nil {
				value.name = *body.Config.Description
			}
			if body.Config.Enabled != nil && !*body.Config.Enabled {
				value.enabled = false
			}
		case r.Method == http.MethodDelete && leaf == "/description":
			value.name = ""
		case r.Method == http.MethodDelete && leaf == "/enabled":
			value.enabled = true
		default:
			t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
			w.WriteHeader(405)
			return
		}
		current[id] = value
		w.WriteHeader(204)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(id int, fields string) {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_interface_lag\" \"test\" {\n lag_id = %d\n%s\n}\noutput \"lag\" { value = fastiron_interface_lag.test }\n", id, fields))
	}
	pending := func() {
		t.Helper()
		if out := run(0, "state", "show", "-no-color", "fastiron_interface_lag.test"); !strings.Contains(out, "persistence_pending = true") {
			t.Fatalf("missing pending state: %s", out)
		}
	}
	check := func(id int, name string, enabled bool) {
		t.Helper()
		want := fmt.Sprintf(`{"enabled":%t,"id":"lag %d","lag_id":%d,"persistence_pending":false,"port_name":%q}`, enabled, id, id, name)
		if out := strings.TrimSpace(run(0, "output", "-json", "lag")); out != want {
			t.Fatalf("interface=%s, want %s", out, want)
		}
		mu.Lock()
		defer mu.Unlock()
		if current[id] != (policy{name: name, enabled: enabled}) || saved != native() {
			t.Fatalf("native=%+v saved differs=%t", current[id], saved != native())
		}
	}

	choose(53, " port_name = \"DESIRED\"\n enabled = false")
	run(0, "init", "-no-color")
	run(1, "apply", "-auto-approve", "-no-color")
	pending()
	mu.Lock()
	failSave = false
	beforeWrites := writes
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(53, "DESIRED", false)
	mu.Lock()
	beforeWrites = writes
	beforeSaves := saves
	mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_interface_lag.test")
	run(0, "import", "-no-color", "fastiron_interface_lag.test", "lag 53")
	mu.Lock()
	if writes != beforeWrites || saves != beforeSaves {
		t.Error("import wrote or saved configuration")
	}
	current[53] = policy{name: "CLI-DRIFT", enabled: true}
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(53, "DESIRED", false)
	run(0, "plan", "-detailed-exitcode", "-no-color")

	choose(53, "")
	run(0, "apply", "-auto-approve", "-no-color")
	check(53, "", true)
	choose(53, " port_name = \"RETRY\"")
	mu.Lock()
	failSave = true
	mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	pending()
	mu.Lock()
	failSave = false
	beforeWrites = writes
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(53, "RETRY", true)
	mu.Lock()
	if writes != beforeWrites {
		t.Error("update persistence retry mutated configuration")
	}
	mu.Unlock()

	choose(54, " port_name = \"OTHER\"\n enabled = false")
	run(0, "apply", "-auto-approve", "-no-color")
	check(54, "OTHER", false)
	mu.Lock()
	if current[53] != (policy{enabled: true}) {
		t.Errorf("replacement left old policy: %+v", current[53])
	}
	failSave = true
	mu.Unlock()
	write("main.tf", base)
	run(1, "apply", "-auto-approve", "-no-color")
	pending()
	mu.Lock()
	parents[54] = false
	failSave = false
	beforeWrites = writes
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if out := run(0, "state", "list"); strings.Contains(out, "fastiron_interface_lag.test") {
		t.Fatalf("deleted policy remains: %s", out)
	}
	mu.Lock()
	if writes != beforeWrites || saved != native() || !strings.Contains(saved, " port-name DETACHED\n disable\n") {
		t.Error("missing-parent retry mutated detached ports or failed to save")
	}
	mu.Unlock()

	choose(53, "")
	run(0, "apply", "-auto-approve", "-no-color")
	mu.Lock()
	parents[53] = false
	beforeWrites = writes
	beforeSaves = saves
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if out := run(0, "state", "list"); strings.Contains(out, "fastiron_interface_lag.test") {
		t.Fatalf("externally deleted parent retains ordinary state: %s", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != beforeWrites || saves != beforeSaves {
		t.Error("refresh wrote or saved configuration")
	}
}
