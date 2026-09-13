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

func TestOpenTofuVEPersistence(t *testing.T) {
	var mu sync.Mutex
	exists, name := false, ""
	savedExists, savedName := false, ""
	falseSave := false
	writes, saves := 0, 0
	native := func(present bool, description string) string {
		text := "ver 09.0.10kT213\nvlan 53 name TRANSIT by port\n"
		if present {
			text += "interface ve 53\n port-name " + description + "\n"
		}
		return text + "end"
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
			return native(exists, name)
		case "show configuration":
			return native(savedExists, savedName)
		case "write memory":
			saves++
			if !falseSave {
				savedExists, savedName = exists, name
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
		case r.Method == "GET" && r.URL.Path == "/restconf/data/network-instances/network-instance=default-vrf/vlans/vlan=53":
			fmt.Fprint(w, `{"openconfig-network-instance:vlan":[{"vlan-id":53,"config":{"vlan-id":53,"name":"TRANSIT"}}]}`)
		case r.Method == "GET" && r.URL.Path == "/restconf/data/interfaces":
			entry := ""
			if exists {
				entry = fmt.Sprintf(`,{"name":"ve 53","config":{"name":"ve 53","type":"iana-if-type:l3ipvlan","description":%q},"openconfig-vlan:routed-vlan":{"config":{"vlan":53}}}`, name)
			}
			fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/1"}%s]}}`, entry)
		case r.Method == "POST" && r.URL.Path == "/restconf/data/interfaces":
			var body struct {
				Interface []struct {
					Config struct {
						Description string `json:"description"`
					} `json:"config"`
				} `json:"interface"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Interface) != 1 {
				t.Error("invalid create payload")
				w.WriteHeader(400)
				return
			}
			exists, name = true, body.Interface[0].Config.Description
			writes++
			w.WriteHeader(201)
		case r.Method == "PUT" && r.URL.Path == "/restconf/data/openconfig-interfaces:interfaces/interface/ve 53/config/description":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid update payload")
				w.WriteHeader(400)
				return
			}
			name = body["openconfig-interfaces:description"]
			writes++
			w.WriteHeader(204)
		case r.Method == "DELETE" && r.URL.Path == "/restconf/data/interfaces/interface=ve 53":
			exists = false
			writes++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(405)
		}
	})
	s := &testSwitch{server: server.REST, sshAddress: server.SSHAddress, knownHosts: server.KnownHosts}
	write, run, base := tofuFixture(t, s)
	choose := func(name string) {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_interface_ve\" \"test\" {\n ve_id = 53\n vlan_id = 53\n port_name = %q\n}\n", name))
	}
	assertState := func(name string, pending bool) {
		t.Helper()
		var state struct {
			Values struct {
				Root struct {
					Resources []struct {
						Address string         `json:"address"`
						Values  map[string]any `json:"values"`
					} `json:"resources"`
				} `json:"root_module"`
			} `json:"values"`
		}
		if err := json.Unmarshal([]byte(run(0, "show", "-json")), &state); err != nil {
			t.Fatal(err)
		}
		for _, resource := range state.Values.Root.Resources {
			if resource.Address != "fastiron_interface_ve.test" {
				continue
			}
			if resource.Values["port_name"] != name || resource.Values["persistence_pending"] != pending || resource.Values["id"] != "ve 53" {
				t.Fatalf("unexpected state: %+v", resource.Values)
			}
			return
		}
		t.Fatal("VE missing from state")
	}
	choose("OLD")
	run(0, "init", "-no-color")
	mu.Lock()
	falseSave = true
	mu.Unlock()
	if output := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(output, "startup configuration does not match running configuration") {
		t.Fatalf("missing create persistence error: %s", output)
	}
	assertState("OLD", true)
	mu.Lock()
	if !exists || savedExists {
		t.Errorf("incorrect partial create: running=%t saved=%t", exists, savedExists)
	}
	falseSave = false
	mu.Unlock()

	run(0, "apply", "-auto-approve", "-no-color")
	assertState("OLD", false)

	choose("NEW")
	mu.Lock()
	falseSave = true
	mu.Unlock()
	output := run(1, "apply", "-auto-approve", "-no-color")
	if !strings.Contains(output, "startup configuration does not match running configuration") {
		t.Fatalf("missing persistence verification error: %s", output)
	}
	assertState("NEW", true)
	mu.Lock()
	if !exists || name != "NEW" || !savedExists || savedName != "OLD" {
		t.Errorf("incorrect partial save state: running=%t/%s saved=%t/%s", exists, name, savedExists, savedName)
	}
	beforeWrites, beforeSaves := writes, saves
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	assertState("NEW", true)
	mu.Lock()
	if writes != beforeWrites || saves != beforeSaves {
		t.Error("refresh mutated switch configuration")
	}
	falseSave = false
	mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertState("NEW", false)
	run(0, "plan", "-detailed-exitcode", "-no-color")
	mu.Lock()
	if writes != beforeWrites || !savedExists || savedName != "NEW" {
		t.Error("retry did not persist the observed update without repeating it")
	}
	falseSave = true
	mu.Unlock()

	run(1, "destroy", "-auto-approve", "-no-color")
	assertState("NEW", true)
	mu.Lock()
	if exists || !savedExists {
		t.Errorf("incorrect partial deletion: running=%t saved=%t", exists, savedExists)
	}
	beforeWrites, beforeSaves = writes, saves
	mu.Unlock()
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	assertState("NEW", true)
	mu.Lock()
	if writes != beforeWrites || saves != beforeSaves {
		t.Error("refresh mutated a pending deletion")
	}
	falseSave = false
	mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	if strings.Contains(run(0, "state", "list"), "fastiron_interface_ve.test") {
		t.Fatal("destroy retained VE state")
	}
	mu.Lock()
	defer mu.Unlock()
	if exists || savedExists || writes != beforeWrites {
		t.Fatalf("delete retry: running=%t saved=%t writes=%d", exists, savedExists, writes)
	}
}
