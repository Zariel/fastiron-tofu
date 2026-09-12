package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

func TestOpenTofuIGMPInventory(t *testing.T) {
	var mu sync.Mutex
	configuration := "ver 09.0.10k\nvlan 4095 name DEFAULT-VLAN by port\nvlan 53 by port\n multicast disable-igmp-snoop\n multicast version 3\nend"
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			mu.Lock()
			defer mu.Unlock()
			return configuration
		default:
			t.Errorf("query issued unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/restconf/data/igmp-mld-snooping/vlans", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("query issued mutation %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"icx-igmp-mld-snooping:vlans":{"vlan":[{"vlan-id":99,"proto":{"vlan-id":99,"igmp":{"config":{"querier-mode":"active","version":2}}}}]}}`)
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_igmp_snooping_vlans" "test" {}
output "vlans" { value = data.fastiron_igmp_snooping_vlans.test.vlans }
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")

	type vlan struct {
		ID      int64   `json:"vlan_id"`
		Mode    *string `json:"querier_mode"`
		Version *int64  `json:"version"`
	}
	var observed map[string]vlan
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "vlans")), &observed); err != nil {
		t.Fatal(err)
	}
	mode, version := "disabled", int64(3)
	want := map[string]vlan{"4095": {ID: 4095}, "53": {ID: 53, Mode: &mode, Version: &version}}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("inventory did not follow native VLANs: %+v", observed)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	mu.Lock()
	configuration = "ver 09.0.10k\nvlan 4095 name DEFAULT-VLAN by port\nend"
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	observed = nil
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "vlans")), &observed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observed, map[string]vlan{"4095": {ID: 4095}}) {
		t.Fatalf("inventory retained a removed VLAN: %+v", observed)
	}
}
