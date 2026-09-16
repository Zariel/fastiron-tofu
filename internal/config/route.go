package config

import (
	"errors"
	"net"
	"net/netip"
	"slices"
)

type routeSyntax struct {
	prefix, mask, gateway string
	distance              int64
	options, ignored      bool
}

// IPv4Route describes a default-VRF gateway route. HasOptions records native
// settings outside the RESTCONF prefix, gateway and administrative distance.
type IPv4Route struct {
	Prefix     netip.Prefix
	NextHop    netip.Addr
	Distance   int64
	HasOptions bool
}

// IPv4Routes excludes routes in other VRFs and interface or null next hops.
func (d *Document) IPv4Routes() ([]IPv4Route, error) {
	type identity struct {
		prefix  netip.Prefix
		gateway netip.Addr
	}
	routes := []IPv4Route{}
	seen := map[identity]bool{}
	for _, command := range d.Commands {
		current, present, err := command.ipv4Route()
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		prefix, gateway := current.Prefix, current.NextHop
		key := identity{prefix: prefix, gateway: gateway}
		if seen[key] {
			return nil, errors.New("native IPv4 route repeats a prefix and gateway")
		}
		seen[key] = true
		routes = append(routes, current)
	}
	return routes, nil
}

func (c Command) ipv4Route() (IPv4Route, bool, error) {
	if c.Parent != -1 || c.kind != staticRoute {
		return IPv4Route{}, false, nil
	}
	if !c.valid {
		return IPv4Route{}, false, errors.New("native IPv4 route is malformed")
	}
	raw := c.route
	if raw.ignored {
		return IPv4Route{}, false, nil
	}
	prefix, err := routePrefix(raw.prefix, raw.mask)
	if err != nil {
		return IPv4Route{}, false, err
	}
	gateway, err := netip.ParseAddr(raw.gateway)
	if err != nil || !gateway.Is4() || gateway.IsUnspecified() || gateway.IsMulticast() {
		return IPv4Route{}, false, errors.New("native IPv4 route gateway is invalid")
	}
	return IPv4Route{Prefix: prefix, NextHop: gateway, Distance: raw.distance, HasOptions: raw.options}, true, nil
}

// CheckIPv4RouteUpdate verifies that only the selected gateway route changed.
func (d *Document) CheckIPv4RouteUpdate(before *Document, prefix netip.Prefix, gateway netip.Addr) error {
	previous, err := before.routeRemaining(prefix, gateway)
	if err != nil {
		return err
	}
	current, err := d.routeRemaining(prefix, gateway)
	if err != nil {
		return err
	}
	if !slices.Equal(previous, current) {
		return errors.New("static route operation changed unrelated native configuration")
	}
	return nil
}

func (d *Document) routeRemaining(prefix netip.Prefix, gateway netip.Addr) ([]string, error) {
	var remaining, routes []string
	found := false
	for _, command := range d.Commands {
		current, present, err := command.ipv4Route()
		if err != nil {
			return nil, err
		}
		if present && current.Prefix == prefix && current.NextHop == gateway {
			if found || current.HasOptions {
				return nil, errors.New("static route operation encountered repeated identity or additional native options")
			}
			found = true
			continue
		}
		if command.Parent == -1 && command.kind == staticRoute {
			routes = append(routes, command.Text)
			continue
		}
		remaining = append(remaining, command.Text)
	}
	// Route display order can change when adding a gateway; each unowned line must remain intact.
	slices.Sort(routes)
	return append(remaining, routes...), nil
}

func routePrefix(raw, mask string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(raw)
	if mask != "" {
		address, addressErr := netip.ParseAddr(raw)
		netmask, maskErr := netip.ParseAddr(mask)
		if addressErr != nil || maskErr != nil || !address.Is4() || !netmask.Is4() {
			return netip.Prefix{}, errors.New("native IPv4 route address or mask is invalid")
		}
		bits, size := net.IPMask(netmask.AsSlice()).Size()
		if size != 32 {
			return netip.Prefix{}, errors.New("native IPv4 route mask is not contiguous")
		}
		prefix, err = netip.PrefixFrom(address, bits), nil
	}
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Addr().IsMulticast() {
		return netip.Prefix{}, errors.New("native IPv4 route prefix is invalid")
	}
	return prefix, nil
}
