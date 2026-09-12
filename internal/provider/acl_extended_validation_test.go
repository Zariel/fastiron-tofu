package provider

import (
	"strings"
	"testing"
)

func TestOpenTofuExtendedACLValidation(t *testing.T) {
	s := newSwitch(t)
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for name, tc := range map[string]struct{ body, message string }{
		"standard number": {`name = "90"`, "100 through 199"},
		"IPv4 protocol zero": {`name = "EDGE"
rule {
 sequence = 10
 action = "permit"
 protocol = 0
}`, "omit it for an IPv4 all-protocol match"},
		"SCTP port": {`name = "EDGE"
rule {
 sequence = 10
 action = "permit"
 protocol = 132
 destination_port = "443"
}`, "port matches require TCP or UDP"},
		"reversed range": {`name = "EDGE"
rule {
 sequence = 10
 action = "permit"
 protocol = 6
 source_port = "2000..1000"
}`, "ascending order"},
		"noncanonical port": {`name = "EDGE"
rule {
 sequence = 10
 action = "permit"
 protocol = 6
 source_port = "080"
}`, "canonical decimal notation"},
		"DSCP overflow": {`name = "EDGE"
rule {
 sequence = 10
 action = "permit"
 dscp = 64
}`, "DSCP match must be 0 through 63"},
	} {
		t.Run(name, func(t *testing.T) {
			write("main.tf", base+"resource \"fastiron_ip_access_list_extended\" \"test\" {\n"+tc.body+"\n}\n")
			if output := run(1, "plan", "-no-color"); !strings.Contains(strings.Join(strings.Fields(output), " "), tc.message) {
				t.Fatalf("missing diagnostic %q: %s", tc.message, output)
			}
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writes != 0 {
		t.Fatalf("invalid ACL performed %d writes", s.writes)
	}
}
