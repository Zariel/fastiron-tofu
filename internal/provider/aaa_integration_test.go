package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuAAAServers(t *testing.T) {
	s := newSwitch(t)
	s.aaaServers = `{"openconfig-system:server-groups":{"server-group":[
 {"name":"radius-default-group","config":{"name":"radius-default-group","type":"openconfig-aaa:RADIUS"},"servers":{"server":[{"address":"192.0.2.53","config":{"address":"192.0.2.53"},"radius":{"config":{"auth-port":1912,"acct-port":1913,"secret-key":"private-server-value","icx-openconfig-aaa-aug:purpose":"accounting-only"}}}]}},
 {"name":"tacacs-default-group","config":{"name":"tacacs-default-group","type":"openconfig-aaa:TACACS"},"servers":{"server":[{"address":"192.0.2.54","config":{"address":"192.0.2.54"},"tacacs":{"config":{"port":49,"secret-key":"","icx-openconfig-aaa-aug:purpose":"default"}}}]}}
 ]}}`
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_aaa_servers" "test" {}
output "servers" { value = data.fastiron_aaa_servers.test.servers }
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	want := `{"radius|192.0.2.53":{"acct_port":1913,"address":"192.0.2.53","auth_port":1912,"kind":"radius","purpose":"accounting-only"},"tacacs|192.0.2.54":{"acct_port":null,"address":"192.0.2.54","auth_port":49,"kind":"tacacs","purpose":"default"}}`
	if got := strings.TrimSpace(run(0, "output", "-json", "servers")); got != want {
		t.Fatalf("AAA output=%s", got)
	}
	if strings.Contains(run(0, "state", "pull"), "private-server-value") {
		t.Fatal("AAA server secret reached OpenTofu state")
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")

	s.mu.Lock()
	s.aaaServers = `{"openconfig-system:server-groups":{}}`
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "servers")); got != "{}" {
		t.Fatalf("empty AAA output=%s", got)
	}
}
