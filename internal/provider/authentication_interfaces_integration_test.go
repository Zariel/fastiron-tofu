package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuAuthenticationInterfaces(t *testing.T) {
	s := newSwitch(t)
	s.authInterfaces = "authentication\n auth-default-vlan 3055\n dot1x enable\n dot1x enable ethe 1/1/9 to 1/1/10\n dot1x port-control auto ethe 1/1/10\n mac-authentication enable\n mac-authentication enable ethe 1/1/9\n!\n"
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_authentication_interfaces" "test" {}
output "authentication" {value=data.fastiron_authentication_interfaces.test.interfaces}
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "authentication")); got != `{"ethernet 1/1/10":{"dot1x_enabled":true,"mac_authentication_enabled":false,"port_control":"auto"},"ethernet 1/1/9":{"dot1x_enabled":true,"mac_authentication_enabled":true,"port_control":"force-authorized"}}` {
		t.Fatalf("authentication interfaces=%s", got)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	s.mu.Lock()
	s.authInterfaces = ""
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "authentication")); got != "{}" {
		t.Fatalf("empty authentication interfaces=%s", got)
	}
}
