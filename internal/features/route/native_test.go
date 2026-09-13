package route

import (
	"net/netip"
	"testing"
)

func TestBannerRoute(t *testing.T) {
	configuration := "ver 09.0.10k\nbanner motd $\nip route 192.0.2.0/24 10.1.2.1\n$\nend"
	if err := routeOptions(configuration, route{Prefix: netip.MustParsePrefix("192.0.2.0/24"), NextHop: netip.MustParseAddr("10.1.2.1")}); err == nil {
		t.Fatal("banner text was accepted as a configured route")
	}
}
