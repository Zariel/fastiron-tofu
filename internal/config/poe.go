package config

import (
	"errors"
	"strings"
)

// PoEPolicy describes configured power allocation, not the class or consumption
// reported by a connected device. A zero power limit means class-based allocation.
type PoEPolicy struct {
	Enabled              bool
	Priority             int64
	PowerByClass         int64
	PowerLimitMilliwatts int64
}

// PoE returns one Ethernet port's inline-power policy and all unowned commands.
// Global port commands cover LAG members whose interface stanzas are unavailable.
func (d *Document) PoE(name string) (PoEPolicy, []string, error) {
	raw, canonical := strings.CutPrefix(name, "ethernet ")
	_, valid := parseEthernetPort(raw)
	if !canonical || !valid {
		return PoEPolicy{}, nil, errors.New("PoE requires a canonical Ethernet identity")
	}
	header, err := d.interfaceHeader(name)
	if err != nil {
		return PoEPolicy{}, nil, err
	}
	policy := PoEPolicy{Enabled: true, Priority: 3}
	var remaining []string
	seen := false
	for i, command := range d.Commands {
		if i == header {
			continue
		}
		owned := header >= 0 && command.Parent == header && (command.kind == inlinePower || command.kind == inlinePowerPort)
		if command.Parent == -1 && command.kind == inlinePowerPort && (command.name == raw || command.name == "") {
			if !command.valid {
				return PoEPolicy{}, nil, errors.New("native global PoE port command is malformed")
			}
			owned = true
		}
		if !owned {
			remaining = append(remaining, command.Text)
			continue
		}
		if seen || !command.valid || (command.Parent >= 0 && command.kind == inlinePowerPort) {
			return PoEPolicy{}, nil, errors.New("native PoE policy is malformed or repeated")
		}
		policy = command.poe
		if policy.Priority < 1 || policy.Priority > 3 || policy.PowerByClass > 4 {
			return PoEPolicy{}, nil, errors.New("native PoE priority or class is invalid")
		}
		if command.poeFields&4 != 0 && (policy.PowerLimitMilliwatts < 1000 || policy.PowerLimitMilliwatts > 95000) {
			return PoEPolicy{}, nil, errors.New("native PoE power limit is invalid")
		}
		seen = true
	}
	return policy, remaining, nil
}
