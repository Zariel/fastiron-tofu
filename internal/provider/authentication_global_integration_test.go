package provider

import (
	"encoding/json"
	"testing"
)

func TestOpenTofuGlobalAuthentication(t *testing.T) {
	s := newSwitch(t)
	s.authInterfaces = "authentication\n auth-order mac-auth dot1x\n auth-default-vlan 3055\n restricted-vlan 3056\n critical-vlan 3057\n voice-vlan 3058\n dot1x guest-vlan 3059\n dot1x enable\n dot1x enable ethe 1/1/10\n mac-authentication enable\n mac-authentication dot1x-disable\n mac-authentication dot1x-override\n re-authentication\n max-sessions 15\n auth-fail-action restricted-vlan\n auth-timeout-action failure\n!\n"
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_authentication" "test" {}
output "global_authentication" { value = data.fastiron_authentication.test }
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	assertOutput := func(want map[string]any) {
		t.Helper()
		var got map[string]any
		if err := json.Unmarshal([]byte(run(0, "output", "-json", "global_authentication")), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("attributes = %v; want %v", got, want)
		}
		for key, expected := range want {
			if got[key] != expected {
				t.Fatalf("%s = %v; want %v", key, got[key], expected)
			}
		}
	}
	assertOutput(map[string]any{
		"dot1x_enabled": true, "mac_authentication_enabled": true, "auth_order": "mac-auth dot1x",
		"auth_default_vlan": float64(3055), "restricted_vlan": float64(3056), "critical_vlan": float64(3057), "voice_vlan": float64(3058), "guest_vlan": float64(3059),
		"max_sessions": float64(15), "re_authentication": true, "mac_dot1x_disable": true, "mac_dot1x_override": true,
		"failure_action": "restricted-vlan", "timeout_action": "failure",
	})
	run(0, "plan", "-detailed-exitcode", "-no-color")

	s.mu.Lock()
	s.authInterfaces = ""
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	assertOutput(map[string]any{
		"dot1x_enabled": false, "mac_authentication_enabled": false, "auth_order": "dot1x mac-auth",
		"auth_default_vlan": nil, "restricted_vlan": nil, "critical_vlan": nil, "voice_vlan": nil, "guest_vlan": nil,
		"max_sessions": float64(2), "re_authentication": false, "mac_dot1x_disable": false, "mac_dot1x_override": false,
		"failure_action": nil, "timeout_action": nil,
	})
	run(0, "plan", "-detailed-exitcode", "-no-color")
}
