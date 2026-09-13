package config

import (
	"errors"
	"strings"
)

type LLDPMode struct{ Receive, Transmit bool }

// LLDPPorts resolves configured directions against a complete Ethernet inventory.
// Explicit ranges must be fully represented so unowned ports cannot disappear
// from configuration comparisons because a RESTCONF inventory is incomplete.
func (d *Document) LLDPPorts(names []string) (map[string]LLDPMode, []string, error) {
	modes := make(map[string]LLDPMode, len(names))
	ports := make(map[string][3]uint64, len(names))
	for _, name := range names {
		raw, canonical := strings.CutPrefix(name, "ethernet ")
		id, valid := parseEthernetPort(raw)
		if !canonical || !valid {
			return nil, nil, errors.New("LLDP inventory contains an invalid Ethernet identity")
		}
		if _, exists := ports[name]; exists {
			return nil, nil, errors.New("LLDP inventory repeats an Ethernet identity")
		}
		ports[name] = id
		modes[name] = LLDPMode{Receive: true, Transmit: true}
	}

	seen := map[string]uint8{}
	var remaining []string
	for _, command := range d.Commands {
		if command.Parent != -1 || command.kind != lldpPorts {
			remaining = append(remaining, command.Text)
			continue
		}
		if !command.valid {
			return nil, nil, errors.New("native LLDP port configuration is malformed")
		}
		mask := uint8(3)
		switch command.direction {
		case "receive":
			mask = 1
		case "transmit":
			mask = 2
		}
		assign := func(name string) error {
			if seen[name]&mask != 0 {
				return errors.New("native LLDP port direction is repeated")
			}
			seen[name] |= mask
			mode := modes[name]
			if mask&1 != 0 {
				mode.Receive = !command.negated
			}
			if mask&2 != 0 {
				mode.Transmit = !command.negated
			}
			modes[name] = mode
			return nil
		}
		names, err := command.resolvePorts(ports)
		if err != nil {
			return nil, nil, err
		}
		for _, name := range names {
			if err := assign(name); err != nil {
				return nil, nil, err
			}
		}
	}
	return modes, remaining, nil
}

func (c Command) resolvePorts(ports map[string][3]uint64) ([]string, error) {
	var names []string
	if c.allPorts {
		for name := range ports {
			names = append(names, name)
		}
		return names, nil
	}
	for _, span := range c.portRanges {
		if span.first[0] != span.last[0] || span.first[1] != span.last[1] || span.first[2] > span.last[2] {
			return nil, errors.New("native LLDP port range is invalid or crosses a slot")
		}
		start := len(names)
		for name, id := range ports {
			if id[0] != span.first[0] || id[1] != span.first[1] || id[2] < span.first[2] || id[2] > span.last[2] {
				continue
			}
			names = append(names, name)
		}
		if uint64(len(names)-start) != span.last[2]-span.first[2]+1 {
			return nil, errors.New("LLDP inventory omits a port referenced by native configuration")
		}
	}
	return names, nil
}
