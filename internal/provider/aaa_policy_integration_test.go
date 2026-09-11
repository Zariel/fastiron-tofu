package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuAAAPolicy(t *testing.T) {
	s := newSwitch(t)
	s.aaaPolicy = `{"openconfig-system:aaa":{"authentication":{"users":{"user":[{"password":"private-user-hash"}]},"icx-openconfig-aaa-aug:login":{"default":["radius","local"]},"icx-openconfig-aaa-aug:dot1x":{"default":"none"}},"authorization":{"icx-openconfig-aaa-aug:coa":{"enable":false,"ignore":{"disable-port":false,"dm-request":true,"flip-port":false,"modify-acl":true,"reauth-host":false}}},"server-groups":{"secret-key":"private-server-key"}}}`
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_aaa" "test" {}
output "aaa" { value=data.fastiron_aaa.test }
`)

	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "aaa")); got != `{"coa_enabled":false,"coa_ignore":["dm-request","modify-acl"],"dot1x_default":"none","login_methods":["radius","local"]}` {
		t.Fatalf("policy=%s", got)
	}
	state := run(0, "state", "pull")
	if strings.Contains(state, "private-user-hash") || strings.Contains(state, "private-server-key") {
		t.Fatal("AAA secrets reached policy state")
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	s.mu.Lock()
	s.aaaPolicy = `{"openconfig-system:aaa":{"authentication":{},"authorization":{}}}`
	s.mu.Unlock()
	if output := run(1, "plan", "-no-color"); !strings.Contains(output, "missing policy settings") {
		t.Fatal("incomplete response did not fail discovery")
	}
}
