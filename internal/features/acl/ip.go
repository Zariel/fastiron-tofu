package acl

import (
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
)

type ipFamily uint8

const (
	ipv4ACL ipFamily = 4
	ipv6ACL ipFamily = 6
)

type optionalInt struct {
	Value   int64
	Present bool
}

type portMatch struct {
	First   int64
	Last    int64
	Present bool
}

type ipRule struct {
	Sequence                    int64
	Action                      string
	Source, Destination         string
	Protocol                    optionalInt
	SourcePort, DestinationPort portMatch
	DSCP, DSCPMark, Priority    optionalInt
	Log                         bool
}

type ipConfig struct {
	Family ipFamily
	Name   string
	Rules  map[int64]ipRule
}

func (f ipFamily) header(name string) string {
	if f == ipv4ACL {
		return "ip access-list extended " + name
	}
	return "ipv6 access-list " + name
}

func (f ipFamily) restType() string {
	if f == ipv4ACL {
		return "ACL_IPV4"
	}
	return "ACL_IPV6"
}

func (f ipFamily) path(name string) string {
	return path.Join(aclPath, "acl-set", url.PathEscape(name), f.restType())
}

func (f ipFamily) address(value string) string {
	if value != "any" {
		return value
	}
	if f == ipv4ACL {
		return "0.0.0.0/0"
	}
	return "::/0"
}

func (p portMatch) String() string {
	if !p.Present {
		return "any"
	}
	first := strconv.FormatInt(p.First, 10)
	if p.First == p.Last {
		return first
	}
	return first + ".." + strconv.FormatInt(p.Last, 10)
}

func validateIP(p ipConfig) error {
	if p.Family != ipv4ACL && p.Family != ipv6ACL {
		return errors.New("IP ACL family must be IPv4 or IPv6")
	}
	if err := validateACLName(p.Name); err != nil {
		return err
	}
	if len(p.Name) > 47 {
		return errors.New("IP ACL names must not exceed 47 bytes")
	}
	if strings.Trim(p.Name, "0123456789") == "" {
		number, err := strconv.ParseInt(p.Name, 10, 64)
		if err != nil || p.Family != ipv4ACL || number < 100 || number > 199 || strconv.FormatInt(number, 10) != p.Name {
			return errors.New("numbered extended IPv4 ACLs require canonical numbers from 100 through 199; IPv6 ACLs require names")
		}
	}

	seen := map[ipRule]bool{}
	for sequence, rule := range p.Rules {
		if sequence < 1 || sequence > 65000 || sequence != rule.Sequence {
			return errors.New("ACL rules require distinct sequence numbers from 1 through 65000")
		}
		if rule.Action != "permit" && rule.Action != "deny" {
			return errors.New("ACL action must be permit or deny")
		}
		if rule.Log && p.Family != ipv6ACL {
			return errors.New("RESTCONF rule logging requires an IPv6 ACL")
		}
		for _, address := range []string{rule.Source, rule.Destination} {
			if address == "any" {
				continue
			}
			prefix, err := netip.ParsePrefix(address)
			if err != nil || prefix.Bits() == 0 || prefix.Masked().String() != address || prefix.Addr().Is4() != (p.Family == ipv4ACL) {
				return fmt.Errorf("ACL sequence %d addresses must be any or canonical IPv%d prefixes", sequence, p.Family)
			}
			if prefix.Addr().Is4In6() {
				return errors.New("IPv4-mapped IPv6 prefixes are unsupported because native save/reload can discard their rules")
			}
		}
		if rule.Protocol.Present && (rule.Protocol.Value < 0 || rule.Protocol.Value > 254 || (p.Family == ipv4ACL && rule.Protocol.Value == 0)) {
			return errors.New("protocol must be 0 through 254; omit it for an IPv4 all-protocol match")
		}
		for _, port := range []portMatch{rule.SourcePort, rule.DestinationPort} {
			if !port.Present {
				continue
			}
			if !rule.Protocol.Present || (rule.Protocol.Value != 6 && rule.Protocol.Value != 17) {
				return errors.New("RESTCONF port matches require TCP or UDP")
			}
			if port.First < 0 || port.Last > 65535 || port.First > port.Last {
				return errors.New("ACL ports must be within 0 through 65535, with range endpoints in ascending order")
			}
		}
		for _, field := range []struct {
			name  string
			value optionalInt
			max   int64
		}{{"DSCP match", rule.DSCP, 63}, {"DSCP marking", rule.DSCPMark, 63}, {"internal priority", rule.Priority, 7}} {
			if field.value.Present && (field.value.Value < 0 || field.value.Value > field.max) {
				return fmt.Errorf("%s must be 0 through %d", field.name, field.max)
			}
		}

		// Sequence is ordering metadata, not a distinct packet rule.
		rule.Sequence = 0
		if seen[rule] {
			return errors.New("RESTCONF cannot represent duplicate IP ACL rules at different sequences")
		}
		seen[rule] = true
	}
	return nil
}

func ipPayload(p ipConfig) map[string]any {
	family := "ipv4"
	if p.Family == ipv6ACL {
		family = "ipv6"
	}
	entries := []any{}
	for _, sequence := range slices.Sorted(maps.Keys(p.Rules)) {
		rule := p.Rules[sequence]
		fields := map[string]any{"source-address": p.Family.address(rule.Source), "destination-address": p.Family.address(rule.Destination)}
		if rule.Protocol.Present {
			fields["protocol"] = rule.Protocol.Value
		}
		if rule.DSCP.Present {
			fields["dscp"] = rule.DSCP.Value
		}
		if rule.DSCPMark.Present {
			fields["dscp-marking"] = map[string]int64{"dscp-marking": rule.DSCPMark.Value}
		}
		if rule.Priority.Present {
			fields["internal-priority-marking"] = map[string]int64{"internal-priority-marking": rule.Priority.Value}
		}
		action := "ACCEPT"
		if rule.Action == "deny" {
			action = "DROP"
		}
		actions := map[string]string{"forwarding-action": action}
		if rule.Log {
			actions["log-action"] = "openconfig-acl:LOG_SYSLOG"
		}
		entry := map[string]any{
			"sequence-id": sequence, "config": map[string]int64{"sequence-id": sequence},
			family:    map[string]any{"config": fields},
			"actions": map[string]any{"config": actions},
		}
		ports := map[string]string{}
		if rule.SourcePort.Present {
			ports["source-port"] = rule.SourcePort.String()
		}
		if rule.DestinationPort.Present {
			ports["destination-port"] = rule.DestinationPort.String()
		}
		if len(ports) > 0 {
			entry["transport"] = map[string]any{"config": ports}
		}
		entries = append(entries, entry)
	}
	return map[string]any{"acl-sets": map[string]any{"acl-set": []any{map[string]any{
		"name": p.Name, "type": p.Family.restType(),
		"config":      map[string]string{"name": p.Name, "type": p.Family.restType()},
		"acl-entries": map[string]any{"acl-entry": entries},
	}}}}
}
