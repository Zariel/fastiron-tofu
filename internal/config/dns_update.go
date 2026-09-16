package config

import (
	"errors"
	"net/netip"
	"slices"
)

// CheckDNSServerUpdate permits only the selected configured address to change.
func (d *Document) CheckDNSServerUpdate(before *Document, address netip.Addr) error {
	previous, previousServers, err := before.dnsRemaining(address)
	if err != nil {
		return err
	}
	current, currentServers, err := d.dnsRemaining(address)
	if err != nil {
		return err
	}
	if !slices.Equal(previous, current) || !slices.Equal(previousServers, currentServers) {
		return errors.New("DNS server operation changed unrelated native configuration")
	}
	return nil
}

func (d *Document) dnsRemaining(address netip.Addr) ([]string, []dnsAddress, error) {
	if _, err := d.DNSServers(); err != nil {
		return nil, nil, err
	}
	var remaining []string
	var servers []dnsAddress
	scopes := make([]string, len(d.Commands))
	for i, c := range d.Commands {
		scopes[i] = c.Text
		if c.Parent >= 0 {
			scopes[i] = scopes[c.Parent] + "\n" + c.Text
		}
		if c.Parent != -1 || c.kind != dnsServers {
			remaining = append(remaining, scopes[i])
			continue
		}
		// FastIron groups addresses on one line; retain other servers' order and origin.
		for _, entry := range c.dns {
			ip := netip.MustParseAddr(entry.address)
			if ip == address && !entry.dynamic {
				continue
			}
			servers = append(servers, dnsAddress{address: ip.String(), dynamic: entry.dynamic})
		}
	}
	return remaining, servers, nil
}
