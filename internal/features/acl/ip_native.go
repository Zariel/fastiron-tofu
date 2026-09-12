package acl

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Native options outside this model must stop reconciliation before a write:
// otherwise importing or refreshing an ACL could silently discard its policy.
func nativeIP(output string, family ipFamily, name string) (*ipConfig, []string, error) {
	var current *ipConfig
	var unowned []string
	active := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.TrimSpace(line) == "!" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			active = line == family.header(name)
			if family == ipv4ACL && line == "ip access-list standard "+name {
				return nil, nil, errors.New("ACL name belongs to a standard IPv4 ACL")
			}
			if active {
				if current != nil {
					return nil, nil, errors.New("duplicate native IP ACL")
				}
				current = &ipConfig{Family: family, Name: name, Rules: map[int64]ipRule{}}
				continue
			}
		}
		if !active {
			unowned = append(unowned, line)
			continue
		}
		rule, err := nativeIPRule(fields, family)
		if err != nil {
			return nil, nil, fmt.Errorf("ACL %s: %w", name, err)
		}
		if _, ok := current.Rules[rule.Sequence]; ok {
			return nil, nil, errors.New("duplicate native ACL sequence")
		}
		current.Rules[rule.Sequence] = rule
	}
	if current != nil {
		if err := validateIP(*current); err != nil {
			return nil, nil, err
		}
	}
	return current, unowned, nil
}

func nativeIPRule(fields []string, family ipFamily) (ipRule, error) {
	var r ipRule
	if len(fields) < 6 || fields[0] != "sequence" {
		return r, errors.New("unsupported native IP ACL rule")
	}
	sequence, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return r, errors.New("invalid native ACL sequence")
	}
	r.Sequence = sequence
	r.Action = fields[2]
	r.Protocol, err = nativeProtocol(fields[3], family)
	if err != nil {
		return r, err
	}
	remaining := fields[4:]
	r.Source, remaining, err = nativeAddress(remaining, family)
	if err != nil {
		return r, err
	}
	r.SourcePort, remaining, err = nativePort(remaining)
	if err != nil {
		return r, err
	}
	r.Destination, remaining, err = nativeAddress(remaining, family)
	if err != nil {
		return r, err
	}
	r.DestinationPort, remaining, err = nativePort(remaining)
	if err != nil {
		return r, err
	}
	for len(remaining) > 0 {
		if len(remaining) < 2 {
			return r, errors.New("unsupported native ACL option")
		}
		var target *optionalInt
		switch remaining[0] {
		case "dscp-matching":
			target = &r.DSCP
		case "dscp-marking":
			target = &r.DSCPMark
		case "internal-priority-marking":
			target = &r.Priority
		default:
			return r, fmt.Errorf("unsupported native ACL option %q", remaining[0])
		}
		if target.Present {
			return r, errors.New("duplicate native ACL option")
		}
		value, err := strconv.ParseInt(remaining[1], 10, 64)
		if err != nil {
			return r, errors.New("invalid native ACL marking")
		}
		*target = optionalInt{value, true}
		remaining = remaining[2:]
	}
	return r, nil
}

func nativeProtocol(value string, family ipFamily) (optionalInt, error) {
	if (family == ipv4ACL && value == "ip") || (family == ipv6ACL && value == "ipv6") {
		return optionalInt{}, nil
	}
	if value == "icmp" && family == ipv6ACL {
		return optionalInt{58, true}, nil
	}
	protocols := map[string]int64{"icmp": 1, "igmp": 2, "tcp": 6, "udp": 17, "ipv6": 41, "rsvp": 46, "gre": 47, "esp": 50, "ah": 51, "ospf": 89, "pim": 103, "sctp": 132, "divert": 254}
	if number, ok := protocols[value]; ok {
		return optionalInt{number, true}, nil
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return optionalInt{}, fmt.Errorf("unsupported native ACL protocol %q", value)
	}
	return optionalInt{number, true}, nil
}

func nativeAddress(fields []string, family ipFamily) (string, []string, error) {
	if len(fields) == 0 {
		return "", nil, errors.New("missing native ACL address")
	}
	if fields[0] == "any" {
		return "any", fields[1:], nil
	}
	count := 1
	if fields[0] == "host" || (family == ipv4ACL && !strings.Contains(fields[0], "/")) {
		count = 2
	}
	if len(fields) < count {
		return "", nil, errors.New("incomplete native ACL address")
	}
	if family == ipv4ACL {
		address, err := standardSource(fields[:count])
		return address, fields[count:], err
	}
	if fields[0] == "host" {
		address, err := netip.ParseAddr(fields[1])
		if err != nil || !address.Is6() || address.Is4In6() || address.Zone() != "" {
			return "", nil, errors.New("invalid native IPv6 ACL host")
		}
		return netip.PrefixFrom(address, 128).String(), fields[2:], nil
	}
	prefix, err := netip.ParsePrefix(fields[0])
	if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() {
		return "", nil, errors.New("invalid native IPv6 ACL prefix")
	}
	if prefix.Bits() == 0 {
		return "any", fields[1:], nil
	}
	return prefix.Masked().String(), fields[1:], nil
}

func nativePort(fields []string) (portMatch, []string, error) {
	if len(fields) == 0 || (fields[0] != "eq" && fields[0] != "range") {
		return portMatch{}, fields, nil
	}
	count := 2
	if fields[0] == "range" {
		count = 3
	}
	if len(fields) < count {
		return portMatch{}, nil, errors.New("incomplete native ACL port expression")
	}
	first, err := nativeService(fields[1])
	if err != nil {
		return portMatch{}, nil, err
	}
	last := first
	if count == 3 {
		last, err = nativeService(fields[2])
		if err != nil {
			return portMatch{}, nil, err
		}
	}
	return portMatch{first, last, true}, fields[count:], nil
}

func nativeService(value string) (int64, error) {
	if number, ok := servicePorts[value]; ok {
		return number, nil
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unsupported native ACL service %q", value)
	}
	return number, nil
}
