package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuStandardACLValidation(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for name, tc := range map[string]struct{ body, message string }{
		"named ACL": {`name = "NAMED"`, "canonical numbers from 1 through 99"},
		"sequence": {`name = "90"
rule {
 sequence = 65001
 action = "permit"
}`, "sequence numbers from 1 through 65000"},
		"duplicate sequence": {`name = "90"
rule {
 sequence = 10
 action = "permit"
}
rule {
 sequence = 10
 action = "deny"
}`, "distinct sequence number"},
		"duplicate filter": {`name = "90"
rule {
 sequence = 10
 action = "permit"
}
rule {
 sequence = 20
 action = "permit"
}`, "duplicate standard ACL rules"},
		"noncanonical prefix": {`name = "90"
rule {
 sequence = 10
 action = "permit"
 source = "192.0.2.1/24"
}`, "canonical IPv4 prefix"},
	} {
		t.Run(name, func(t *testing.T) {
			write("main.tf", base+"resource \"fastiron_ip_access_list_standard\" \"test\" {\n"+tc.body+"\n}\n")
			if output := run(1, "plan", "-no-color"); !strings.Contains(output, tc.message) {
				t.Fatalf("missing diagnostic %q: %s", tc.message, output)
			}
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writes != 0 {
		t.Fatalf("invalid ACL configuration performed %d writes", s.writes)
	}
}
