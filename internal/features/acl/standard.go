package acl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type standardRule struct {
	Sequence int64
	Action   string
	Source   string
}

type standardConfig struct {
	Name  string
	Rules map[int64]standardRule
}

func readStandard(ctx context.Context, d *fastiron.Device, name string) (*standardConfig, error) {
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	current, _, err := standardConfiguration(ctx, d, name)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fastiron.ErrNotFound
	}
	return current, nil
}

// nativeStandard rejects unrepresented rule options rather than silently
// dropping them during reconciliation. All other ACLs and bindings stay unowned.
func nativeStandard(document *nativeconfig.Document, name string) (*standardConfig, []string, error) {
	var current *standardConfig
	var unowned []string
	active := false
	for _, command := range document.Commands {
		line := command.Text
		fields := command.Fields
		if len(fields) == 0 || strings.TrimSpace(line) == "!" {
			continue
		}
		topLevel := command.Parent == -1
		switch {
		case line == "ip access-list standard "+name:
			if current != nil {
				return nil, nil, errors.New("duplicate native standard ACL")
			}
			current = &standardConfig{Name: name, Rules: map[int64]standardRule{}}
			active = true
			continue
		case topLevel:
			if (strings.HasPrefix(line, "ip access-list ") || strings.HasPrefix(line, "ipv6 access-list ") || strings.HasPrefix(line, "mac access-list ")) && fields[len(fields)-1] == name {
				return nil, nil, errors.New("ACL name already belongs to a different ACL family")
			}
			active = false
		}
		if !active {
			unowned = append(unowned, line)
			continue
		}
		if len(fields) < 4 || fields[0] != "sequence" {
			return nil, nil, errors.New("standard ACL contains native settings outside supported RESTCONF ownership")
		}
		sequence, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || sequence < 1 {
			return nil, nil, errors.New("invalid native ACL sequence")
		}
		if fields[2] != "permit" && fields[2] != "deny" {
			return nil, nil, errors.New("unsupported native standard ACL action")
		}
		source, err := standardSource(fields[3:])
		if err != nil {
			return nil, nil, err
		}
		if _, exists := current.Rules[sequence]; exists {
			return nil, nil, errors.New("duplicate native ACL sequence")
		}
		current.Rules[sequence] = standardRule{Sequence: sequence, Action: fields[2], Source: source}
	}
	return current, unowned, nil
}

func standardSource(fields []string) (string, error) {
	if len(fields) == 1 && fields[0] == "any" {
		return "any", nil
	}
	if len(fields) == 2 && fields[0] == "host" {
		address, err := netip.ParseAddr(fields[1])
		if err != nil || !address.Is4() {
			return "", errors.New("invalid native ACL host address")
		}
		return netip.PrefixFrom(address, 32).String(), nil
	}
	if len(fields) == 1 {
		prefix, err := netip.ParsePrefix(fields[0])
		if err != nil || !prefix.Addr().Is4() {
			return "", errors.New("unsupported native ACL source expression")
		}
		if prefix.Bits() == 0 {
			return "any", nil
		}
		return prefix.Masked().String(), nil
	}
	if len(fields) != 2 {
		return "", errors.New("standard ACL contains unsupported source options")
	}
	address, err := netip.ParseAddr(fields[0])
	if err != nil || !address.Is4() {
		return "", errors.New("invalid native ACL source address")
	}
	wildcard, err := netip.ParseAddr(fields[1])
	if err != nil || !wildcard.Is4() {
		return "", errors.New("invalid native ACL wildcard mask")
	}
	octets := wildcard.As4()
	bits, size := net.IPMask([]byte{^octets[0], ^octets[1], ^octets[2], ^octets[3]}).Size()
	if size != 32 {
		return "", errors.New("noncontiguous ACL wildcard masks are not represented by RESTCONF prefixes")
	}
	if bits == 0 {
		return "any", nil
	}
	return netip.PrefixFrom(address, bits).Masked().String(), nil
}

func validateStandard(p standardConfig) error {
	number, err := strconv.ParseInt(p.Name, 10, 64)
	if err != nil || number < 1 || number > 99 || strconv.FormatInt(number, 10) != p.Name {
		return errors.New("RESTCONF standard ACL names must be canonical numbers from 1 through 99")
	}
	seen := map[[2]string]bool{}
	for sequence, rule := range p.Rules {
		if sequence < 1 || sequence > 65000 || sequence != rule.Sequence {
			return errors.New("ACL rules require distinct sequence numbers from 1 through 65000")
		}
		if rule.Action != "permit" && rule.Action != "deny" {
			return errors.New("ACL action must be permit or deny")
		}
		match := [2]string{rule.Action, rule.Source}
		if seen[match] {
			return errors.New("RESTCONF cannot represent duplicate standard ACL rules at different sequences")
		}
		seen[match] = true
		if rule.Source == "any" {
			continue
		}
		prefix, err := netip.ParsePrefix(rule.Source)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix.Masked().String() != rule.Source {
			return fmt.Errorf("ACL sequence %d source must be any or a canonical IPv4 prefix", sequence)
		}
	}
	return nil
}
