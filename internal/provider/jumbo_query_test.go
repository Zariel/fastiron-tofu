package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuJumboQuery(t *testing.T) {
	var enabled, active, missing atomic.Bool
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			flag := ""
			if enabled.Load() {
				flag = "jumbo\n"
			}
			return "ver 09.0.10kT213\n" + flag + "end"
		default:
			t.Errorf("query issued %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/restconf/data/jumbo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("query attempted %s", r.Method)
			w.WriteHeader(405)
			return
		}
		if missing.Load() {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintf(w, `{"icx-openconfig-jumbo:jumbo":{"config":{"enabled":%t},"operation-state":{"enabled":%t}}}`, !enabled.Load(), active.Load())
	})
	write, run, base := tofuFixture(t, &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts})
	write("main.tf", base+`data "fastiron_jumbo" "test" {}
output "jumbo" { value = data.fastiron_jumbo.test }
`)
	run(0, "init", "-no-color")
	for _, expected := range []struct{ enabled, active, reload bool }{
		{false, false, false},
		{true, false, true},
		{true, true, false},
		{false, true, true},
		{false, false, false},
	} {
		enabled.Store(expected.enabled)
		active.Store(expected.active)
		run(0, "apply", "-auto-approve", "-no-color")
		var got struct {
			Enabled bool `json:"enabled"`
			Active  bool `json:"active_enabled"`
			Reload  bool `json:"reload_required"`
		}
		if err := json.Unmarshal([]byte(run(0, "output", "-json", "jumbo")), &got); err != nil {
			t.Fatal(err)
		}
		if got.Enabled != expected.enabled || got.Active != expected.active || got.Reload != expected.reload {
			t.Fatalf("jumbo=%+v expected=%+v", got, expected)
		}
		run(0, "plan", "-detailed-exitcode", "-no-color")
	}

	missing.Store(true)
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "Cannot read jumbo configuration") {
		t.Fatalf("missing diagnostic: %s", output)
	}
}
