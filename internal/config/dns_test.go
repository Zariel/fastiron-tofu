package config_test

import (
	"net/netip"
	"os"
	"slices"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestDNSServers(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []string
	}{
		{"empty", "", []string{}},
		{"multiple", "ip dns server-address 192.0.2.54 192.0.2.53\nipv6 dns server-address 2001:DB8::53", []string{"192.0.2.53", "192.0.2.54", "2001:db8::53"}},
		{"DHCP learned", "ip dns server-address 20.20.20.8 10.10.10.5(dynamic)", []string{"20.20.20.8"}},
		{"dynamic only", "ip dns server-address 10.10.10.5(dynamic)", []string{}},
		{"other scope", "interface ve 5\n ip dns server-address 192.0.2.53", []string{}},
		{"domain list", "ip dns domain-list example.test", []string{}},
		{"whitespace", "ip\tdns server-address\t192.0.2.53", []string{"192.0.2.53"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := configtest.Parse(t, "ver 09.0.10k\n"+tc.body+"\nend").DNSServers()
			want := make([]netip.Addr, len(tc.want))
			for i, address := range tc.want {
				want[i] = netip.MustParseAddr(address)
			}
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("servers=%v error=%v want=%v", got, err, want)
			}
		})
	}
}

func TestDNSServersMalformed(t *testing.T) {
	for _, body := range []string{
		"ip dns server-address",
		"ip dns server-address invalid",
		"ip dns server-address 2001:db8::53",
		"ipv6 dns server-address 192.0.2.53",
		"ipv6 dns server-address ::ffff:192.0.2.53",
		"ip dns server-address 192.0.2.53 extra",
		"ip dns server-address 192.0.2.53 192.0.2.53",
		"ip dns server-address 192.0.2.256",
		"ipv6 dns server-address fe80::53%1",
		"ip dns server-address 0.0.0.0",
		"ip dns server-address 224.0.0.53",
	} {
		if got, err := configtest.Parse(t, "ver 09.0.10k\n"+body+"\nend").DNSServers(); err == nil {
			t.Errorf("accepted %q: %v", body, got)
		}
	}
}

func TestCapturedDNSServers(t *testing.T) {
	raw, err := os.ReadFile("testdata/dns/configured.conf")
	if err != nil {
		t.Fatal(err)
	}
	got, err := configtest.Parse(t, string(raw)).DNSServers()
	want := []netip.Addr{netip.MustParseAddr("192.0.2.53"), netip.MustParseAddr("198.51.100.53"), netip.MustParseAddr("2001:db8::53")}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("servers=%v error=%v want=%v", got, err, want)
	}
}
