package acl

import (
	"errors"
	"strconv"
	"strings"
)

func nativeMAC(output, name string) (*macConfig, []string, error) {
	var current *macConfig
	var unowned []string
	active := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.TrimSpace(line) == "!" {
			continue
		}
		if line == "mac access-list "+name {
			if current != nil {
				return nil, nil, errors.New("duplicate native MAC ACL")
			}
			current = &macConfig{Name: name, Rules: []macRule{}}
			active = true
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			active = false
		}
		if !active {
			unowned = append(unowned, line)
			continue
		}
		rule, err := nativeMACRule(fields)
		if err != nil {
			return nil, nil, err
		}
		current.Rules = append(current.Rules, rule)
	}
	if current != nil {
		if err := validateMAC(*current); err != nil {
			return nil, nil, err
		}
	}
	return current, unowned, nil
}

func nativeMACRule(fields []string) (macRule, error) {
	var rule macRule
	if len(fields) < 3 || (fields[0] != "permit" && fields[0] != "deny") {
		return rule, errors.New("MAC ACL contains unsupported native settings")
	}
	rule.Action = fields[0]
	source, remaining, err := nativeMACMatch(fields[1:])
	if err != nil {
		return rule, err
	}
	destination, remaining, err := nativeMACMatch(remaining)
	if err != nil {
		return rule, err
	}
	rule.Source, rule.Destination = source, destination
	for len(remaining) > 0 {
		switch remaining[0] {
		case "ether-type":
			if rule.EtherType.Present || len(remaining) < 2 {
				return rule, errors.New("invalid native MAC ACL EtherType")
			}
			value, err := strconv.ParseInt(remaining[1], 16, 64)
			if err != nil || value < 0x600 || value > 0xffff {
				return rule, errors.New("invalid native MAC ACL EtherType")
			}
			rule.EtherType = optionalInt{Value: value, Present: true}
			remaining = remaining[2:]
		case "log":
			if rule.Log {
				return rule, errors.New("duplicate native MAC ACL logging option")
			}
			rule.Log = true
			remaining = remaining[1:]
		default:
			return rule, errors.New("MAC ACL contains options outside supported RESTCONF ownership")
		}
	}
	return rule, nil
}

func nativeMACMatch(fields []string) (macMatch, []string, error) {
	var match macMatch
	if len(fields) > 0 && fields[0] == "any" {
		return match, fields[1:], nil
	}
	if len(fields) < 2 {
		return match, nil, errors.New("incomplete native MAC ACL match")
	}
	match, err := parseMACMatch(fields[0], fields[1])
	return match, fields[2:], err
}
