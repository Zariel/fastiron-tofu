package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuUsers(t *testing.T) {
	s := newSwitch(t)
	s.users = `{"openconfig-system:users":{"user":[{"username":"super","config":{"username":"super","password":"$6$private-user-hash","icx-openconfig-aaa-aug:privilege":0}},{"username":"viewer","config":{"username":"viewer","password":"private-password","icx-openconfig-aaa-aug:privilege":5}}]}}`
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_aaa_users" "test" {}
output "users" { value=data.fastiron_aaa_users.test.users }
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "users")); got != `{"super":0,"viewer":5}` {
		t.Fatalf("user metadata=%s", got)
	}
	state := run(0, "state", "pull")
	if strings.Contains(state, "private-user-hash") || strings.Contains(state, "private-password") {
		t.Fatal("user password material reached state")
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	s.users = `{"openconfig-system:users":{}}`
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "users")); got != "{}" {
		t.Fatalf("empty user metadata=%s", got)
	}
}
