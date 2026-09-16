package provider

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
)

// This simulator exercises the real plugin protocol, not hardware compatibility.
// Its running and startup maps provide an independent observation path.
type testSwitch struct {
	stp            *stpSwitch
	stpPorts       *stpPortSwitch
	authInterfaces string
	auth           *authenticationSwitch
	policy         *policySwitch
	aaaPolicy      string
	aaaServers     string
	users          string
	userAccounts   *userSwitch
	aaa            *aaaSwitch

	ospf *ospfSwitch

	routes *routeSwitch

	managementAddresses, startupManagementAddresses map[string]int

	lags                            *lagSwitch
	startupLAG                      string
	addresses                       map[string]int
	addressChild                    bool
	ve                              map[string]any
	veChild                         bool
	mu                              sync.Mutex
	running, startup                map[int]string
	memberships, startupMemberships map[int]string
	ethernet, startupEthernet       map[string]any
	child                           bool
	failSave                        bool
	falseSave                       bool
	missingVLANEndpoint             bool
	ambiguous                       bool
	writes                          int
	server                          *httptest.Server
	sshAddress, knownHosts          string
	dns                             map[string]bool
	startupLLDP, startupLLDPPort    bool
	lldp, lldpPort                  bool
	poe, startupPoE                 bool
}

func newSwitch(t *testing.T) *testSwitch {
	t.Helper()
	s := &testSwitch{memberships: map[int]string{}, running: map[int]string{}, startup: map[int]string{}, ethernet: map[string]any{"name": "ethernet 1/1/2", "description": "manual port", "enabled": true, "mtu": float64(9000)}}
	s.startupEthernet = maps.Clone(s.ethernet)
	s.dns = map[string]bool{}
	s.addresses = map[string]int{}
	s.lldp, s.lldpPort = true, true
	s.startupLLDP, s.startupLLDPPort = true, true
	s.poe, s.startupPoE = true, true
	transport := testswitch.New(t, s.command)
	transport.HandleFunc("/", s.restconf)
	s.server = transport.REST
	s.sshAddress, s.knownHosts = transport.SSHAddress, transport.KnownHosts
	return s
}

func (s *testSwitch) command(command string) (output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if command == "show running-config" {
			if !s.poe {
				output = strings.Replace(output, "interface ethernet 1/1/2\n", "interface ethernet 1/1/2\n no inline power\n", 1)
			}
			output = strings.TrimSuffix(output, "end") + lldpConfiguration(s.lldp, s.lldpPort) + "end"
		}
		if command == "show configuration" {
			if !s.startupPoE {
				output = strings.Replace(output, "interface ethernet 1/1/2\n", "interface ethernet 1/1/2\n no inline power\n", 1)
			}
			output = strings.TrimSuffix(output, "end") + lldpConfiguration(s.startupLLDP, s.startupLLDPPort) + "end"
		}
	}()
	switch command {
	case "show version":
		return "UNIT 1: compiled on Sep 10 2026 labeled as SPR09010k\nSW: Version 09.0.10kT213\nHW: ICX7150-C12P"
	case "write memory":
		if s.failSave {
			return "Error: failed to create startup-config"
		}
		if s.falseSave {
			return "Write startup-config done."
		}
		policyUnchanged := s.policy == nil || s.policy.running.native() == s.policy.startup.native()
		if s.policy != nil {
			s.policy.startup = s.policy.running
		}
		usersUnchanged := s.userAccounts == nil || maps.Equal(s.userAccounts.running, s.userAccounts.startup)
		if s.userAccounts != nil {
			s.userAccounts.startup = maps.Clone(s.userAccounts.running)
		}
		aaaUnchanged := s.aaa == nil || maps.Equal(s.aaa.running, s.aaa.startup)
		if s.aaa != nil {
			s.aaa.startup = maps.Clone(s.aaa.running)
		}
		unchanged := s.poe == s.startupPoE && s.lldp == s.startupLLDP && s.lldpPort == s.startupLLDPPort && policyUnchanged && usersUnchanged && aaaUnchanged && maps.Equal(s.running, s.startup) && maps.Equal(s.ethernet, s.startupEthernet) && maps.Equal(s.memberships, s.startupMemberships) && maps.Equal(s.managementAddresses, s.startupManagementAddresses)
		if s.lags != nil {
			lagConfig := s.lags.configuration()
			unchanged = unchanged && lagConfig == s.startupLAG
			s.startupLAG = lagConfig
		}
		if s.routes != nil {
			unchanged = unchanged && maps.Equal(s.routes.running, s.routes.startup)
			s.routes.startup = maps.Clone(s.routes.running)
		}
		if s.ospf != nil {
			unchanged = unchanged && maps.EqualFunc(s.ospf.areas, s.ospf.startup, slices.Equal[[]string])
			s.ospf.startup = map[string][]string{}
			for id, names := range s.ospf.areas {
				s.ospf.startup[id] = slices.Clone(names)
			}
		}
		if s.stp != nil {
			unchanged = unchanged && maps.Equal(s.stp.running, s.stp.startup)
			s.stp.startup = maps.Clone(s.stp.running)
		}
		if s.stpPorts != nil {
			unchanged = unchanged && maps.Equal(s.stpPorts.running, s.stpPorts.startup)
			s.stpPorts.startup = maps.Clone(s.stpPorts.running)
		}
		if s.auth != nil {
			unchanged = unchanged && maps.Equal(s.auth.running, s.auth.startup)
			s.auth.startup = maps.Clone(s.auth.running)
		}
		s.startupPoE = s.poe
		s.startupLLDP, s.startupLLDPPort = s.lldp, s.lldpPort
		s.startupManagementAddresses = maps.Clone(s.managementAddresses)
		s.startup = maps.Clone(s.running)
		s.startupEthernet = maps.Clone(s.ethernet)
		s.startupMemberships = maps.Clone(s.memberships)
		if unchanged {
			return "write memory completed. No new config is added."
		}
		return "Write startup-config done."
	case "show running-config":
		text := s.configuration(s.running, s.ethernet, s.memberships)
		if s.auth != nil {
			text = strings.TrimSuffix(text, "end") + authenticationConfig(s.auth.running, s.auth.extra) + "end"
		}
		if s.authInterfaces != "" {
			text = strings.TrimSuffix(text, "end") + s.authInterfaces + "end"
		}
		if s.policy != nil {
			text = strings.TrimSuffix(text, "end") + s.policy.running.native() + s.policy.extra + "end"
		}
		if s.userAccounts != nil {
			text = strings.TrimSuffix(text, "end") + userConfig(s.userAccounts.running, s.userAccounts.protected) + "end"
		}
		if s.aaa != nil {
			text = strings.TrimSuffix(text, "end") + aaaConfiguration(s.aaa.running) + "end"
		}
		if s.stpPorts != nil {
			text = stpPortConfiguration(text, s.stpPorts.running)
		}
		if s.stp != nil {
			text = stpConfiguration(text, s.stp.running, s.stp.extra)
		}
		if s.ospf != nil {
			text = strings.TrimSuffix(text, "end") + ospfConfiguration(s.ospf.areas, s.ospf.areaOptions, s.ospf.interfaceOptions, s.ospf.hiddenBinding) + "end"
		}
		if s.routes != nil {
			text = strings.TrimSuffix(text, "end") + routeConfiguration(s.routes.running, s.routes.extra) + "end"
		}
		if s.lags != nil {
			text = strings.TrimSuffix(text, "end") + s.lags.configuration() + "end"
		}
		return strings.TrimSuffix(text, "end") + managementConfiguration(s.managementAddresses) + "end"
	case "show configuration":
		if s.auth != nil {
			return strings.TrimSuffix(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), "end") + authenticationConfig(s.auth.startup, s.auth.extra) + "end"
		}
		if s.policy != nil {
			return strings.TrimSuffix(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), "end") + s.policy.startup.native() + s.policy.extra + "end"
		}
		if s.userAccounts != nil {
			return strings.TrimSuffix(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), "end") + userConfig(s.userAccounts.startup, s.userAccounts.protected) + "end"
		}
		if s.aaa != nil {
			return strings.TrimSuffix(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), "end") + aaaConfiguration(s.aaa.startup) + "end"
		}
		if s.stpPorts != nil {
			return stpPortConfiguration(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), s.stpPorts.startup)
		}
		if s.stp != nil {
			return stpConfiguration(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), s.stp.startup, s.stp.extra)
		}
		if s.ospf != nil {
			return strings.TrimSuffix(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), "end") + ospfConfiguration(s.ospf.startup, s.ospf.areaOptions, s.ospf.interfaceOptions, s.ospf.hiddenBinding) + "end"
		}
		if s.routes != nil {
			return strings.TrimSuffix(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), "end") + routeConfiguration(s.routes.startup, s.routes.extra) + "end"
		}
		return strings.TrimSuffix(s.configuration(s.startup, s.startupEthernet, s.startupMemberships), "end") + s.startupLAG + managementConfiguration(s.startupManagementAddresses) + "end"
	case "skip-page-display":
		return ""
	default:
		return "% Invalid command"
	}
}

func (s *testSwitch) configuration(vlans map[int]string, ethernet map[string]any, memberships map[int]string) string {
	text := "ver 09.0.10kT213\n!\n"
	ids := []int{}
	for id := range vlans {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		text += fmt.Sprintf("vlan %d", id)
		if vlans[id] != "" {
			text += " name " + vlans[id]
		}
		text += " by port\n"
		if memberships[id] != "" {
			text += " " + memberships[id] + " ethe 1/1/2\n"
		}
		if s.child {
			text += " tagged ethe 1/1/2\n"
		}
		text += "!\n"
	}
	text += "interface ethernet 1/1/2\n"
	if ethernet["description"] != "" {
		text += " port-name " + ethernet["description"].(string) + "\n"
	}
	if ethernet["enabled"] == false {
		text += " disable\n"
	}
	text += " ip mtu 9000\n!\n"
	if s.ve != nil {
		text += "interface ve 53\n"
		if name, _ := s.ve["description"].(string); name != "" {
			text += " port-name " + name + "\n"
		}
		for _, ip := range slices.Sorted(maps.Keys(s.addresses)) {
			text += fmt.Sprintf(" ip address %s/%d\n", ip, s.addresses[ip])
		}
		if s.veChild {
			text += " ip address 192.0.2.1 255.255.255.0\n"
		}
		text += "!\n"
	}
	return text + "end"
}

func (s *testSwitch) restconf(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auth != nil && (r.URL.Path == "/restconf/data/authentication/config" || strings.HasPrefix(r.URL.Path, "/restconf/data/authentication/config/")) {
		s.auth.rest(w, r)
		return
	}
	if s.policy != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/system/aaa") {
		s.policy.rest(w, r)
		return
	}
	if s.aaaPolicy != "" && r.Method == "GET" && r.URL.Path == "/restconf/data/system/aaa" {
		fmt.Fprint(w, s.aaaPolicy)
		return
	}
	if s.userAccounts != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/system/aaa/authentication/users") {
		s.userAccounts.rest(w, r)
		return
	}
	if s.users != "" && r.Method == "GET" && r.URL.Path == "/restconf/data/system/aaa/authentication/users" {
		fmt.Fprint(w, s.users)
		return
	}
	if s.aaa != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/system/aaa/server-groups") {
		s.aaa.rest(w, r)
		return
	}
	if s.aaaServers != "" && r.Method == "GET" && r.URL.Path == "/restconf/data/system/aaa/server-groups" {
		fmt.Fprint(w, s.aaaServers)
		return
	}
	if s.stpPorts != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/stp/interfaces") {
		s.stpPorts.rest(w, r)
		return
	}
	if s.lags != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/interfaces") {
		s.lags.rest(w, r)
		return
	}
	if s.routes != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/network-instances/network-instance=default-vrf/protocols") {
		s.routes.rest(w, r)
		return
	}
	if s.ospf != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/network-instances/network-instance=default-vrf/protocols") {
		s.ospf.rest(w, r)
		return
	}
	if s.stp != nil && strings.HasPrefix(r.URL.Path, "/restconf/data/stp") {
		s.stp.rest(w, r)
		return
	}
	const collection = "/restconf/data/network-instances/network-instance=default-vrf/vlans"
	if strings.HasPrefix(r.URL.EscapedPath(), "/restconf/data/interfaces/interface=ve%2053/routed-vlan/") || strings.HasPrefix(r.URL.EscapedPath(), "/restconf/data/interfaces/interface=management%201/subinterfaces/subinterface=0/") {
		s.addressREST(w, r)
		return
	}
	if (r.Method == "POST" && r.URL.Path == "/restconf/data/interfaces") || r.URL.EscapedPath() == "/restconf/data/interfaces/interface=ve%2053" || r.URL.Path == "/restconf/data/openconfig-interfaces:interfaces/interface/ve 53/config/description" {
		s.veREST(w, r)
		return
	}
	if strings.HasSuffix(r.URL.EscapedPath(), "/ethernet/poe") || strings.HasSuffix(r.URL.EscapedPath(), "/ethernet/poe/config") {
		s.poeREST(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/restconf/data/lldp") {
		s.lldpREST(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/restconf/data/system/dns") {
		s.dnsREST(w, r)
		return
	}
	if strings.HasPrefix(r.URL.EscapedPath(), "/restconf/data/interfaces/interface=") {
		s.membershipREST(w, r)
		return
	}
	if s.missingVLANEndpoint && strings.HasPrefix(r.URL.Path, collection) {
		w.WriteHeader(404)
		return
	}
	if r.Method == "GET" && r.URL.Path == collection {
		entries := []any{}
		for id, name := range s.running {
			entries = append(entries, map[string]any{"vlan-id": id, "config": map[string]any{"vlan-id": id, "name": name}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-network-instance:vlans": map[string]any{"vlan": entries}})
		return
	}
	if r.URL.Path == "/restconf/data/interfaces" {
		if r.Method == "GET" {
			entries := []any{map[string]any{"name": "ethernet 1/1/2", "config": s.ethernet, "state": s.ethernet, "openconfig-if-ethernet:ethernet": map[string]any{"icx-openconfig-if-poe-aug:poe": map[string]any{"config": map[string]any{"enabled": s.poe}, "state": map[string]any{"power-used": "7000.0", "power-class": 4}}}}}
			if s.ve != nil {
				entries = append(entries, map[string]any{"name": "ve 53", "config": s.ve, "openconfig-vlan:routed-vlan": map[string]any{"config": map[string]any{"vlan": 53}}})
			}
			if s.stpPorts != nil {
				entries = append(entries, map[string]any{"name": "ethernet 1/1/3", "config": map[string]any{"name": "ethernet 1/1/3", "description": "NEIGHBOR", "enabled": true}})
			}
			if s.auth != nil {
				entries = append(entries, map[string]any{"name": "ethernet 1/1/4", "config": map[string]any{"name": "ethernet 1/1/4", "description": "", "enabled": true}})
			}
			json.NewEncoder(w).Encode(map[string]any{"openconfig-interfaces:interfaces": map[string]any{"interface": entries}})
			return
		}
		if r.Method == "PATCH" {
			var body struct {
				Interfaces struct {
					Interface []struct {
						Name   string         `json:"name"`
						Config map[string]any `json:"config"`
					} `json:"interface"`
				} `json:"interfaces"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Interfaces.Interface) != 1 {
				w.WriteHeader(400)
				return
			}
			entry := body.Interfaces.Interface[0]
			if entry.Name == "ve 53" && s.ve != nil {
				maps.Copy(s.ve, entry.Config)
			} else if entry.Name == "ethernet 1/1/2" {
				maps.Copy(s.ethernet, entry.Config)
			} else {
				w.WriteHeader(400)
				return
			}
			s.writes++
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(400)
		return
	}
	if r.Method == "GET" && strings.HasPrefix(r.URL.Path, collection+"/vlan=") {
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, collection+"/vlan="))
		name, ok := s.running[id]
		if !ok {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"ietf-restconf:errors":{"error":[{"error-tag":"invalid-value","error-app-tag":"data-invalid","error-info":{"error-number":388}}]}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-network-instance:vlan": []any{map[string]any{"vlan-id": id, "config": map[string]any{"vlan-id": id, "name": name}}}})
		return
	}
	if r.URL.Path == collection && (r.Method == "POST" || r.Method == "PATCH") {
		var payload map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			w.WriteHeader(400)
			return
		}
		if r.Method == "PATCH" {
			if json.Unmarshal(payload["vlans"], &payload) != nil {
				w.WriteHeader(400)
				return
			}
		}
		var entries []struct {
			ID     int `json:"vlan-id"`
			Config struct {
				ID   int    `json:"vlan-id"`
				Name string `json:"name"`
			} `json:"config"`
		}
		if json.Unmarshal(payload["vlan"], &entries) != nil || len(entries) != 1 || entries[0].ID != entries[0].Config.ID {
			w.WriteHeader(400)
			return
		}
		s.running[entries[0].ID] = entries[0].Config.Name
		s.writes++
		if s.ambiguous {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, collection+"/vlan=") {
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, collection+"/vlan="))
		delete(s.running, id)
		s.writes++
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(400)
}

func tofuFixture(t *testing.T, s *testSwitch) (func(string, string), func(int, ...string) string, string) {
	t.Helper()
	tofu := os.Getenv("TOFU_BINARY")
	if tofu == "" {
		var err error
		tofu, err = exec.LookPath("tofu")
		if err != nil {
			t.Skip("OpenTofu is required; run inside nix develop")
		}
	}
	dir := t.TempDir()
	mirror := filepath.Join(dir, "mirror")
	plugins := filepath.Join(mirror, "registry.opentofu.org", "zariel", "fastiron", "0.0.1", runtime.GOOS+"_"+runtime.GOARCH)
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(plugins, "terraform-provider-fastiron"), "../../cmd/terraform-provider-fastiron")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("tofurc", fmt.Sprintf("provider_installation { filesystem_mirror { path = %q } }\n", mirror))
	_, httpPort, _ := net.SplitHostPort(strings.TrimPrefix(s.server.URL, "https://"))
	_, sshPort, _ := net.SplitHostPort(s.sshAddress)
	write("ca.pem", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.server.Certificate().Raw})))
	write("known_hosts", s.knownHosts)
	base := fmt.Sprintf(`terraform {
  required_providers {
    fastiron = { source = "zariel/fastiron", version = "0.0.1" }
  }
}
provider "fastiron" {
  host = "127.0.0.1"
  username = "automation"
  password = "test-password"
  persistence_mode = "after_each_write"
  restconf {
    port = %s
    ca_certificate = file("${path.module}/ca.pem")
  }
  ssh {
    port = %s
    known_hosts = file("${path.module}/known_hosts")
  }
}
data "fastiron_capabilities" "switch" {}
output "firmware" { value = data.fastiron_capabilities.switch.firmware }
`, httpPort, sshPort)
	run := func(want int, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tofu, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TF_CLI_CONFIG_FILE="+filepath.Join(dir, "tofurc"), "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1")
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != want {
			t.Fatalf("tofu %v: exit %d, want %d\n%s", args, code, want, out)
		}
		return string(out)
	}
	return write, run, base
}

func TestOpenTofu(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	resetPort := false
	config := func(name *string) {
		resource := "resource \"fastiron_vlan\" \"test\" {\n vlan_id = 53\n"
		if name != nil {
			resource += fmt.Sprintf(" name = %q\n", *name)
		}
		port := "resource \"fastiron_interface_ethernet\" \"test\" {\n port = \"1/1/2\"\n"
		if !resetPort {
			port += " port_name = \"UPLINK\"\n enabled = false\n"
		}
		write("main.tf", base+resource+"}\n"+port+"}\n")
	}
	check := func(name string, exists bool) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		for label, vlans := range map[string]map[int]string{"running": s.running, "startup": s.startup} {
			got, ok := vlans[53]
			if ok != exists || (exists && got != name) {
				t.Fatalf("%s VLAN = %q, exists %v; want %q, exists %v", label, got, ok, name, exists)
			}
		}
	}
	checkPort := func(name string, enabled bool) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		for label, port := range map[string]map[string]any{"running": s.ethernet, "startup": s.startupEthernet} {
			if port["description"] != name || port["enabled"] != enabled || port["mtu"] != float64(9000) {
				t.Fatalf("%s port: %#v", label, port)
			}
		}
	}
	name := "INFRA"
	config(&name)
	run(0, "init", "-no-color")
	run(0, "validate", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check("INFRA", true)
	checkPort("UPLINK", false)
	if got := strings.TrimSpace(run(0, "output", "-json", "firmware")); got != `"09.0.10k"` {
		t.Fatalf("firmware output: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.missingVLANEndpoint = true
	s.mu.Unlock()
	run(1, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.missingVLANEndpoint = false
	s.mu.Unlock()
	s.mu.Lock()
	s.running[53] = "MANUAL"
	s.ethernet["description"] = "MANUAL PORT"
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check("INFRA", true)
	checkPort("UPLINK", false)
	run(0, "state", "rm", "fastiron_vlan.test")
	run(0, "import", "-no-color", "fastiron_vlan.test", "vlan 53")
	run(0, "state", "rm", "fastiron_interface_ethernet.test")
	run(0, "import", "-no-color", "fastiron_interface_ethernet.test", "ethernet 1/1/2")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	resetPort = true
	config(nil)
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check("", true)
	checkPort("", true)
	run(0, "plan", "-detailed-exitcode", "-no-color")
	name = "AFTER-TIMEOUT"
	config(&name)
	s.mu.Lock()
	s.ambiguous = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
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
	var partial map[string]any
	for _, resource := range document.Values.Root.Resources {
		if resource.Address == "fastiron_vlan.test" {
			partial = resource.Values
		}
	}
	if partial["name"] != name || partial["persistence_pending"] != true {
		t.Fatalf("partial VLAN state lost: %v", partial)
	}

	s.mu.Lock()
	if s.running[53] != name || s.startup[53] != "" {
		t.Errorf("partial VLAN write persisted: running=%q startup=%q", s.running[53], s.startup[53])
	}
	writes := s.writes
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(name, true)
	s.mu.Lock()
	if s.writes != writes {
		t.Error("retry repeated an already converged VLAN mutation")
	}
	s.ambiguous = false
	s.failSave = true
	s.mu.Unlock()
	name = "SAVE-RETRY"
	config(&name)
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(name, true)
	run(0, "plan", "-detailed-exitcode", "-no-color")
	name = "VERIFY-SAVE"
	config(&name)
	s.mu.Lock()
	s.falseSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.falseSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(name, true)
	s.mu.Lock()
	s.child = true
	s.mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	check(name, true)
	s.mu.Lock()
	s.child = false
	s.failSave = true
	s.mu.Unlock()
	run(1, "destroy", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "destroy", "-auto-approve", "-no-color")
	check("", false)
	checkPort("", true)
	config(&name)
	s.mu.Lock()
	s.running[54] = "NEIGHBOR"
	s.memberships[54] = "tagged"
	s.mu.Unlock()
	membership := func(tagging string) {
		write("membership.tf", fmt.Sprintf(`resource "fastiron_vlan_membership" "test" {
 vlan_id = fastiron_vlan.test.vlan_id
 interface = fastiron_interface_ethernet.test.name
 tagging = %q
}
`, tagging))
	}
	checkMembership := func(want map[int]string) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !maps.Equal(s.memberships, want) || !maps.Equal(s.startupMemberships, want) {
			t.Fatalf("memberships: running=%v startup=%v want=%v", s.memberships, s.startupMemberships, want)
		}
	}
	membership("tagged")
	run(0, "apply", "-auto-approve", "-no-color")
	checkMembership(map[int]string{53: "tagged", 54: "tagged"})
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_vlan_membership.test")
	run(0, "import", "-no-color", "fastiron_vlan_membership.test", "vlan 53|ethernet 1/1/2|tagged")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	delete(s.memberships, 53)
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	checkMembership(map[int]string{53: "tagged", 54: "tagged"})
	write("membership.tf", "")
	s.mu.Lock()
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	checkMembership(map[int]string{54: "tagged"})
	membership("untagged")
	s.mu.Lock()
	s.memberships[54] = "untagged"
	s.mu.Unlock()
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "another untagged VLAN") {
		t.Fatalf("missing ownership diagnostic: %s", out)
	}
	s.mu.Lock()
	conflictPreserved := s.memberships[54] == "untagged" && len(s.memberships) == 1
	delete(s.memberships, 54)
	s.mu.Unlock()
	if !conflictPreserved {
		t.Fatal("conflicting untagged membership was changed")
	}
	run(0, "apply", "-auto-approve", "-no-color")
	checkMembership(map[int]string{53: "untagged"})
	run(0, "plan", "-detailed-exitcode", "-no-color")
	write("membership.tf", "")
	run(0, "apply", "-auto-approve", "-no-color")
	checkMembership(map[int]string{})
	run(0, "destroy", "-auto-approve", "-no-color")
	base = strings.Replace(base, `persistence_mode = "after_each_write"`, `persistence_mode = "manual"`, 1)
	name = "MANUAL-SAVE"
	config(&name)
	write("save.tf", `resource "fastiron_configuration_save" "test" {
  revision = "first"
  depends_on = [fastiron_vlan.test, fastiron_interface_ethernet.test]
}
`)
	run(0, "apply", "-auto-approve", "-no-color")
	check(name, true)
	name = "NEXT-REVISION"
	config(&name)
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	running, saved := s.running[53], s.startup[53]
	s.mu.Unlock()
	if running != "NEXT-REVISION" || saved != "MANUAL-SAVE" {
		t.Fatalf("manual persistence: running=%q, startup=%q", running, saved)
	}
	write("save.tf", `resource "fastiron_configuration_save" "test" {
  revision = "second"
  depends_on = [fastiron_vlan.test, fastiron_interface_ethernet.test]
}
`)
	run(0, "apply", "-auto-approve", "-no-color")
	check(name, true)
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.dns["192.0.2.54"] = true
	s.mu.Unlock()
	write("dns.tf", `resource "fastiron_ip_dns_server" "test" { address = "192.0.2.53" }
data "fastiron_ip_dns_servers" "test" { depends_on = [fastiron_ip_dns_server.test] }
output "dns" { value = data.fastiron_ip_dns_servers.test.addresses }
`)
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "dns")); got != `["192.0.2.53","192.0.2.54"]` {
		t.Fatalf("DNS collection: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_ip_dns_server.test")
	run(0, "import", "-no-color", "fastiron_ip_dns_server.test", "ip dns server-address 192.0.2.53")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	delete(s.dns, "192.0.2.53")
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	dnsPreserved := s.dns["192.0.2.53"] && s.dns["192.0.2.54"]
	s.mu.Unlock()
	if !dnsPreserved {
		t.Fatal("DNS drift correction did not preserve both servers")
	}
	write("dns.tf", "")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	dnsNeighborPreserved := len(s.dns) == 1 && s.dns["192.0.2.54"]
	s.mu.Unlock()
	if !dnsNeighborPreserved {
		t.Fatal("DNS deletion changed an unrelated server")
	}
	write("lldp.tf", `resource "fastiron_lldp" "test" { enabled = false }
resource "fastiron_lldp_interface" "test" {
 interface = "ethernet 1/1/2"
 enabled = false
}
data "fastiron_lldp_interfaces" "test" { depends_on = [fastiron_lldp_interface.test] }
output "lldp" { value = data.fastiron_lldp_interfaces.test.interfaces }
`)
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "lldp")); got != `{"ethernet 1/1/2":false,"ethernet 1/1/3":true}` {
		t.Fatalf("LLDP collection: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_lldp.test", "fastiron_lldp_interface.test")
	run(0, "import", "-no-color", "fastiron_lldp.test", "lldp")
	run(0, "import", "-no-color", "fastiron_lldp_interface.test", "lldp|ethernet 1/1/2")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.lldp = true
	s.lldpPort = true
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	global, port := s.lldp, s.lldpPort
	s.mu.Unlock()
	if global || port {
		t.Fatal("LLDP drift was not corrected")
	}
	write("lldp.tf", `resource "fastiron_lldp" "test" {}
resource "fastiron_lldp_interface" "test" { interface = "ethernet 1/1/2" }
`)
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	global, port = s.lldp, s.lldpPort
	s.mu.Unlock()
	if !global || !port {
		t.Fatal("omitted LLDP settings did not reset to enabled")
	}
	s.mu.Lock()
	s.lldp = false
	s.lldpPort = false
	s.mu.Unlock()
	write("lldp.tf", "")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	global, port = s.lldp, s.lldpPort
	s.mu.Unlock()
	if !global || !port {
		t.Fatal("LLDP destroy did not reset to enabled")
	}
	base = strings.Replace(base, `persistence_mode = "manual"`, `persistence_mode = "after_each_write"`, 1)
	config(&name)
	write("poe.tf", `resource "fastiron_interface_poe" "test" {
 interface = "ethernet 1/1/2"
 enabled = false
}
data "fastiron_poe_interfaces" "test" { depends_on = [fastiron_interface_poe.test] }
output "poe" { value = data.fastiron_poe_interfaces.test.interfaces }
`)
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "poe")); got != `{"ethernet 1/1/2":{"enabled":false,"power_allocated_milliwatts":null,"power_by_class":0,"power_class":4,"power_limit_milliwatts":0,"power_used_milliwatts":7000,"priority":3}}` {
		t.Fatalf("PoE collection: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_interface_poe.test")
	run(0, "import", "-no-color", "fastiron_interface_poe.test", "poe|ethernet 1/1/2")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.poe = true
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	poe := s.poe
	s.mu.Unlock()
	if poe {
		t.Fatal("PoE drift was not corrected")
	}
	write("poe.tf", `resource "fastiron_interface_poe" "test" { interface = "ethernet 1/1/2" }
`)
	s.mu.Lock()
	s.falseSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	if !s.poe || s.startupPoE {
		t.Error("failed PoE save lost the distinction between running and startup")
	}
	writes = s.writes
	s.falseSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	if s.writes != writes || !s.startupPoE {
		t.Error("PoE persistence retry repeated a mutation or failed to save")
	}
	s.mu.Unlock()
	s.mu.Lock()
	poe = s.poe
	s.mu.Unlock()
	if !poe {
		t.Fatal("omitted PoE enable did not restore default")
	}
	s.mu.Lock()
	s.poe = false
	s.mu.Unlock()
	write("poe.tf", "")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	poe = s.poe
	s.mu.Unlock()
	if !poe {
		t.Fatal("PoE destroy did not restore default")
	}
	base = strings.Replace(base, `persistence_mode = "after_each_write"`, `persistence_mode = "manual"`, 1)
	config(&name)
	write("ve.tf", `resource "fastiron_interface_ve" "test" {
 ve_id = 53
 vlan_id = fastiron_vlan.test.vlan_id
 port_name = "TRANSIT"
}
`)
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	veName := s.ve["description"]
	s.mu.Unlock()
	if veName != "TRANSIT" {
		t.Fatal("VE creation did not set description")
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_interface_ve.test")
	run(0, "import", "-no-color", "fastiron_interface_ve.test", "ve 53")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.ve["description"] = "DRIFT"
	s.veChild = true
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	veName = s.ve["description"]
	child := s.veChild
	s.mu.Unlock()
	if veName != "TRANSIT" || !child {
		t.Fatal("VE update did not preserve child configuration")
	}
	write("ve.tf", `resource "fastiron_interface_ve" "test" {
 ve_id = 53
 vlan_id = fastiron_vlan.test.vlan_id
}
`)
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	veName = s.ve["description"]
	s.mu.Unlock()
	if veName != "" {
		t.Fatal("VE name omission did not reset description")
	}
	write("ve.tf", "")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "VE has child configuration") {
		t.Fatalf("missing child guard: %s", out)
	}
	s.mu.Lock()
	exists := s.ve != nil
	s.veChild = false
	s.mu.Unlock()
	if !exists {
		t.Fatal("VE child guard removed interface")
	}
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	exists = s.ve != nil
	_, vlanExists := s.running[53]
	s.mu.Unlock()
	if exists || !vlanExists {
		t.Fatal("VE destroy did not preserve parent VLAN")
	}
	veConfig := `resource "fastiron_interface_ve" "addresses" {
 ve_id = 53
 vlan_id = fastiron_vlan.test.vlan_id
}
`
	addressConfig := func(cidr string) {
		write("addresses.tf", veConfig+fmt.Sprintf(`resource "fastiron_interface_ipv4_address" "test" {
 interface = fastiron_interface_ve.addresses.name
 address = %q
}
resource "fastiron_interface_ipv6_address" "test" {
 interface = fastiron_interface_ve.addresses.name
 address = "2001:db8::1/64"
}
data "fastiron_interface_addresses" "test" {
 interface = fastiron_interface_ve.addresses.name
 depends_on = [fastiron_interface_ipv4_address.test,fastiron_interface_ipv6_address.test]
}
output "addresses" { value = data.fastiron_interface_addresses.test.addresses }
`, cidr))
	}
	write("addresses.tf", veConfig)
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.addresses["198.51.100.1"] = 30
	s.mu.Unlock()
	addressConfig("192.0.2.129/24")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "addresses")); got != `["192.0.2.129/24","198.51.100.1/30","2001:db8::1/64"]` {
		t.Fatalf("address collection: %s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_interface_ipv4_address.test", "fastiron_interface_ipv6_address.test")
	run(0, "import", "-no-color", "fastiron_interface_ipv4_address.test", "ve 53|ipv4|192.0.2.129/24")
	run(0, "import", "-no-color", "fastiron_interface_ipv6_address.test", "ve 53|ipv6|2001:db8::1/64")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.addresses["192.0.2.129"] = 25
	delete(s.addresses, "2001:db8::1")
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	addresses := maps.Clone(s.addresses)
	s.mu.Unlock()
	if !maps.Equal(addresses, map[string]int{"192.0.2.129": 24, "198.51.100.1": 30, "2001:db8::1": 64}) {
		t.Fatalf("address drift correction: %v", addresses)
	}
	addressConfig("192.0.2.129/25")
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	addresses = maps.Clone(s.addresses)
	s.mu.Unlock()
	if !maps.Equal(addresses, map[string]int{"192.0.2.129": 25, "198.51.100.1": 30, "2001:db8::1": 64}) {
		t.Fatalf("prefix replacement: %v", addresses)
	}
	s.mu.Lock()
	s.addressChild = true
	s.mu.Unlock()
	write("addresses.tf", veConfig)
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "VRRP child configuration") {
		t.Fatalf("missing VRRP guard: %s", out)
	}
	s.mu.Lock()
	addresses = maps.Clone(s.addresses)
	s.addressChild = false
	s.mu.Unlock()
	if len(addresses) != 3 {
		t.Fatal("guarded address deletion removed entries")
	}
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	addresses = maps.Clone(s.addresses)
	s.mu.Unlock()
	if !maps.Equal(addresses, map[string]int{"198.51.100.1": 30}) {
		t.Fatalf("address cleanup changed neighbor: %v", addresses)
	}
}
