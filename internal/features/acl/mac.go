package acl

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
)

type macAddress [6]byte

func parseMAC(value string) (macAddress, error) {
	address, err := net.ParseMAC(value)
	if err != nil || len(address) != 6 {
		return macAddress{}, errors.New("expected a 48-bit MAC address")
	}
	return macAddress(address), nil
}

func (a macAddress) String() string { return net.HardwareAddr(a[:]).String() }

type macMatch struct {
	Address macAddress
	Mask    macAddress
}

type macRule struct {
	Action              string
	Source, Destination macMatch
	EtherType           optionalInt
	Log                 bool
}

type macConfig struct {
	Name  string
	Rules []macRule
}

// MAC rule order persists natively, but REST entry IDs can change after reload.
// IDs belong to an observed REST entry, never to the desired configuration.
type macEntry struct {
	ID   int64
	Rule macRule
}

func macPath(name string) string {
	return path.Join(aclPath, "acl-set", url.PathEscape(name), "ACL_L2")
}

func validateMAC(config macConfig) error {
	if err := validateACLName(config.Name); err != nil {
		return err
	}
	seen := map[macRule]bool{}
	for index, rule := range config.Rules {
		if rule.Action != "permit" && rule.Action != "deny" {
			return errors.New("MAC ACL action must be permit or deny")
		}
		if rule.EtherType.Present && (rule.EtherType.Value < 0x600 || rule.EtherType.Value > 0xffff) {
			return errors.New("MAC ACL EtherType must be between 1536 and 65535")
		}
		for _, match := range []macMatch{rule.Source, rule.Destination} {
			if match.Mask == (macAddress{}) && match.Address != (macAddress{}) {
				return errors.New("a zero MAC mask must use the any address")
			}
		}
		if seen[rule] {
			return fmt.Errorf("MAC ACL rule %d duplicates an earlier rule", index+1)
		}
		seen[rule] = true
	}
	return nil
}

func macPayload(name string, entries []macEntry) map[string]any {
	rules := make([]any, 0, len(entries))
	for _, entry := range entries {
		rule := entry.Rule
		match := map[string]any{
			"source-mac": rule.Source.Address.String(), "source-mac-mask": rule.Source.Mask.String(),
			"destination-mac": rule.Destination.Address.String(), "destination-mac-mask": rule.Destination.Mask.String(),
		}
		if rule.EtherType.Present {
			match["ethertype"] = rule.EtherType.Value
		}
		action := "ACCEPT"
		if rule.Action == "deny" {
			action = "DROP"
		}
		actions := map[string]any{"forwarding-action": action}
		if rule.Log {
			actions["log-action"] = "openconfig-acl:LOG_SYSLOG"
		}
		rules = append(rules, map[string]any{
			"sequence-id": entry.ID, "config": map[string]any{"sequence-id": entry.ID},
			"l2": map[string]any{"config": match}, "actions": map[string]any{"config": actions},
		})
	}
	acl := map[string]any{
		"name": name, "type": "ACL_L2", "config": map[string]any{"name": name, "type": "ACL_L2"},
		"acl-entries": map[string]any{"acl-entry": rules},
	}
	return map[string]any{"acl-sets": map[string]any{"acl-set": []any{acl}}}
}
