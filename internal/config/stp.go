package config

import (
	"errors"
	"fmt"
	"slices"
)

// STPVLAN describes a VLAN's explicitly configured spanning-tree policy.
type STPVLAN struct {
	VLANID   int64
	Mode     string
	Priority int64
}

// STPVLAN returns nil when the VLAN has no explicit spanning-tree policy.
// Additional settings are rejected because deleting the mode would erase them.
func (d *Document) STPVLAN(id int64) (*STPVLAN, error) {
	if id < 1 || id > 4095 {
		return nil, errors.New("invalid spanning-tree VLAN identity")
	}
	header := -1
	for i, command := range d.Commands {
		if command.Parent != -1 || command.kind != vlanHeader || command.number != id {
			continue
		}
		if !command.valid || header != -1 {
			return nil, errors.New("native configuration has an invalid or repeated VLAN header")
		}
		header = i
	}
	if header == -1 {
		return nil, nil
	}

	var policy *STPVLAN
	modeSeen, prioritySeen := false, false
	for _, command := range d.Commands {
		if command.Parent != header || (command.kind != spanningTree && command.kind != stpEdge && command.kind != stpRoot) {
			continue
		}
		if !command.valid || command.kind != spanningTree {
			return nil, errors.New("native VLAN has malformed or unsupported spanning-tree settings")
		}
		if command.number < 0 || command.number > 65535 {
			return nil, errors.New("invalid native spanning-tree bridge priority")
		}
		if policy == nil {
			policy = &STPVLAN{VLANID: id, Mode: command.family, Priority: 32768}
		}
		if policy.Mode != command.family {
			return nil, errors.New("native VLAN contains conflicting spanning-tree modes")
		}
		if command.options {
			if prioritySeen {
				return nil, errors.New("native VLAN repeats its spanning-tree priority")
			}
			prioritySeen = true
			policy.Priority = command.number
			continue
		}
		if modeSeen {
			return nil, errors.New("native VLAN repeats its spanning-tree mode")
		}
		modeSeen = true
	}
	return policy, nil
}

// STPVLANs reports explicit native VLAN policies, ordered by VLAN ID.
// Unsupported spanning-tree settings are rejected rather than reported as defaults.
func (d *Document) STPVLANs() ([]STPVLAN, error) {
	policies := []STPVLAN{}
	for _, command := range d.Commands {
		if command.Parent != -1 || command.kind != vlanHeader {
			continue
		}
		if !command.valid {
			return nil, errors.New("native configuration has an invalid VLAN header")
		}
		policy, err := d.STPVLAN(command.number)
		if err != nil {
			return nil, fmt.Errorf("cannot read spanning-tree VLAN %d: %w", command.number, err)
		}
		if policy != nil {
			policies = append(policies, *policy)
		}
	}
	slices.SortFunc(policies, func(a, b STPVLAN) int { return int(a.VLANID - b.VLANID) })
	return policies, nil
}

// STPInterface contains independently configured interface protection flags.
type STPInterface struct {
	AdminEdge bool
	BPDUGuard bool
	RootGuard bool
}

// STPInterfaces reports explicit native interface flags. Absent entries use defaults.
func (d *Document) STPInterfaces() (map[string]STPInterface, error) {
	policies := map[string]STPInterface{}
	seen := map[string]uint8{}
	headers := map[string]int{}
	bits := map[kind]uint8{stpEdge: 1, stpRoot: 2, stpBPDU: 4}
	for i, command := range d.Commands {
		if command.Parent != -1 || command.kind != interfaceStanza || !command.valid {
			continue
		}
		if _, exists := headers[command.name]; exists {
			headers[command.name] = -1
		} else {
			headers[command.name] = i
		}
	}
	for _, command := range d.Commands {
		bit := bits[command.kind]
		if bit == 0 || command.Parent < 0 {
			continue
		}
		header := d.Commands[command.Parent]
		if header.kind != interfaceStanza || header.Parent != -1 {
			continue
		}
		if !header.valid || !command.valid {
			return nil, errors.New("native spanning-tree interface policy is malformed")
		}
		name := header.name
		if headers[name] == -1 {
			return nil, errors.New("native spanning-tree interface has repeated stanzas")
		}
		if seen[name]&bit != 0 {
			return nil, errors.New("native spanning-tree interface repeats a protection flag")
		}
		seen[name] |= bit

		policy := policies[name]
		enabled := !command.negated
		switch command.kind {
		case stpEdge:
			policy.AdminEdge = enabled
		case stpRoot:
			policy.RootGuard = enabled
		case stpBPDU:
			policy.BPDUGuard = enabled
		}
		policies[name] = policy
	}
	return policies, nil
}

// STPInterface selects one interface's flags and preserves every unowned command.
func (d *Document) STPInterface(name string) (STPInterface, []string, error) {
	policies, err := d.STPInterfaces()
	if err != nil {
		return STPInterface{}, nil, err
	}
	header, err := d.interfaceHeader(name)
	if err != nil {
		return STPInterface{}, nil, err
	}
	var remaining []string
	for i, command := range d.Commands {
		if i == header {
			continue
		}
		owned := header >= 0 && command.Parent == header && (command.kind == stpEdge || command.kind == stpRoot || command.kind == stpBPDU)
		if !owned {
			remaining = append(remaining, command.Text)
		}
	}
	return policies[name], remaining, nil
}
