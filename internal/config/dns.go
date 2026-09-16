package config

import (
	"errors"
	"net/netip"
	"slices"
)

type dnsAddress struct {
	address string
	dynamic bool
}

// DNSServers returns configured server addresses, excluding DHCP-learned entries.
func (d *Document) DNSServers() ([]netip.Addr, error) {
	servers := []netip.Addr{}
	seen := map[netip.Addr]bool{}
	for _, c := range d.Commands {
		if c.kind != dnsServers || c.Parent != -1 {
			continue
		}
		if !c.valid {
			return nil, errors.New("native DNS server configuration is malformed")
		}
		for _, entry := range c.dns {
			ip, err := netip.ParseAddr(entry.address)
			if err != nil || ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() || ip.Is4() != (c.family == "ip") {
				return nil, errors.New("native DNS server address is invalid or has the wrong address family")
			}
			if entry.dynamic {
				continue
			}
			if seen[ip] {
				return nil, errors.New("native DNS server address is repeated")
			}
			seen[ip] = true
			servers = append(servers, ip)
		}
	}
	slices.SortFunc(servers, netip.Addr.Compare)
	return servers, nil
}
