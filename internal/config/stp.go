package config

import "errors"

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
		if command.Parent != header || command.kind != spanningTree {
			continue
		}
		if !command.valid {
			return nil, errors.New("VLAN has malformed or additional spanning-tree settings; remove them before destroying its spanning-tree configuration")
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
