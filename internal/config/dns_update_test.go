package config_test

import (
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestCapturedDNSServerUpdate(t *testing.T) {
	before, err := os.ReadFile("testdata/dns/baseline.conf")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile("testdata/dns/created.conf")
	if err != nil {
		t.Fatal(err)
	}
	if err := configtest.Parse(t, string(after)).CheckDNSServerUpdate(configtest.Parse(t, string(before)), netip.MustParseAddr("198.18.62.53")); err != nil {
		t.Fatal(err)
	}
}

func TestDNSServerUpdate(t *testing.T) {
	const before = `ver 09.0.10k
ip dns server-address 192.0.2.53 192.0.2.54 192.0.2.55 192.0.2.56(dynamic)
ipv6 dns server-address 2001:DB8::53
ip dns domain-list example.test
interface ethernet 1/1/9
 port-name KEEP
interface ethernet 1/1/10
end`
	for _, tc := range []struct {
		name, after string
		allowed     bool
	}{
		{"unchanged", before, true},
		{"owned removal", strings.Replace(before, "192.0.2.53 ", "", 1), true},
		{"line grouping", strings.Replace(before, "192.0.2.53 ", "192.0.2.53\nip dns server-address ", 1), true},
		{"IPv6 spelling", strings.Replace(before, "2001:DB8::53", "2001:db8:0:0:0:0:0:53", 1), true},
		{"neighbor removal", strings.Replace(before, "192.0.2.54 ", "", 1), false},
		{"new neighbor", strings.Replace(before, "192.0.2.54 ", "192.0.2.57 192.0.2.54 ", 1), false},
		{"neighbor order", strings.Replace(before, "192.0.2.54 192.0.2.55", "192.0.2.55 192.0.2.54", 1), false},
		{"dynamic removal", strings.Replace(before, " 192.0.2.56(dynamic)", "", 1), false},
		{"dynamic ownership", strings.Replace(before, "192.0.2.53 ", "192.0.2.53(dynamic) ", 1), false},
		{"domain list", strings.Replace(before, "example.test", "other.test", 1), false},
		{"unrelated interface", strings.Replace(before, "KEEP", "CHANGED", 1), false},
		{"moved child", strings.Replace(before, " port-name KEEP\ninterface ethernet 1/1/10", "interface ethernet 1/1/10\n port-name KEEP", 1), false},
		{"malformed", strings.Replace(before, "192.0.2.54", "invalid", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := configtest.Parse(t, tc.after).CheckDNSServerUpdate(configtest.Parse(t, before), netip.MustParseAddr("192.0.2.53"))
			if (err == nil) != tc.allowed {
				t.Fatalf("update error=%v; allowed=%t", err, tc.allowed)
			}
		})
	}
}
