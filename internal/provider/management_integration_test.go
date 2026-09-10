package provider

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

func managementConfiguration(addresses map[string]int) string {
	if len(addresses) == 0 {
		return ""
	}
	text := "interface management 1\n"
	ips := []string{}
	for ip := range addresses {
		ips = append(ips, ip)
	}
	slices.Sort(ips)
	for _, ip := range ips {
		family := "ip"
		if strings.Contains(ip, ":") {
			family = "ipv6"
		}
		text += fmt.Sprintf(" %s address %s/%d\n", family, ip, addresses[ip])
	}
	return text + "!\n"
}

func TestOpenTofuManagementAddresses(t *testing.T) {
	s := newSwitch(t)
	s.managementAddresses = map[string]int{"10.1.2.111": 24}
	s.startupManagementAddresses = maps.Clone(s.managementAddresses)
	s.addresses["192.0.2.1"] = 30
	write, run, base := tofuFixture(t, s)
	config := func(bits int) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_interface_ipv4_address" "test" {
 interface = "management 1"
 address = "198.18.0.201/%d"
}
resource "fastiron_interface_ipv6_address" "test" {
 interface = "management 1"
 address = "2001:db8:1::201/64"
}
data "fastiron_interface_addresses" "test" {
 interface = "management 1"
 depends_on = [fastiron_interface_ipv4_address.test, fastiron_interface_ipv6_address.test]
}
output "addresses" { value = data.fastiron_interface_addresses.test.addresses }
`, bits))
	}
	check := func(want map[string]int) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !maps.Equal(s.managementAddresses, want) || !maps.Equal(s.startupManagementAddresses, want) {
			t.Errorf("management addresses: running=%v startup=%v want=%v", s.managementAddresses, s.startupManagementAddresses, want)
		}
		if !maps.Equal(s.addresses, map[string]int{"192.0.2.1": 30}) {
			t.Error("management update changed VE addresses")
		}
	}
	config(24)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int{"10.1.2.111": 24, "198.18.0.201": 24, "2001:db8:1::201": 64})
	if got := strings.TrimSpace(run(0, "output", "-json", "addresses")); got != `["10.1.2.111/24","198.18.0.201/24","2001:db8:1::201/64"]` {
		t.Fatalf("management discovery: %s", got)
	}
	run(0, "state", "rm", "fastiron_interface_ipv4_address.test", "fastiron_interface_ipv6_address.test")
	run(0, "import", "-no-color", "fastiron_interface_ipv4_address.test", "management 1|ipv4|198.18.0.201/24")
	run(0, "import", "-no-color", "fastiron_interface_ipv6_address.test", "management 1|ipv6|2001:db8:1::201/64")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	config(25)
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int{"10.1.2.111": 24, "198.18.0.201": 25, "2001:db8:1::201": 64})
	s.mu.Lock()
	delete(s.managementAddresses, "2001:db8:1::201")
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int{"10.1.2.111": 24, "198.18.0.201": 25, "2001:db8:1::201": 64})

	write("main.tf", base)
	s.mu.Lock()
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string]int{"10.1.2.111": 24})
	run(0, "plan", "-detailed-exitcode", "-no-color")
}
