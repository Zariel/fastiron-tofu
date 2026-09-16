package config

import (
	"errors"
	"net"
	"net/netip"
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
		if command.Parent != -1 || command.kind != staticRoute {
			continue
		}
		if !command.valid {
			return nil, errors.New("native IPv4 route is malformed")
		}
		raw := command.route
		if raw.ignored {
			continue
		}
		prefix, err := routePrefix(raw.prefix, raw.mask)
		if err != nil {
			return nil, err
		}
		gateway, err := netip.ParseAddr(raw.gateway)
		if err != nil || !gateway.Is4() || gateway.IsUnspecified() || gateway.IsMulticast() {
			return nil, errors.New("native IPv4 route gateway is invalid")
		}
		key := identity{prefix: prefix, gateway: gateway}
		if seen[key] {
			return nil, errors.New("native IPv4 route repeats a prefix and gateway")
		}
		seen[key] = true
		routes = append(routes, IPv4Route{Prefix: prefix, NextHop: gateway, Distance: raw.distance, HasOptions: raw.options})
	}
	return routes, nil
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
