package config

import (
	"errors"
	"strconv"
	"strings"
)

type VE struct {
	Exists      bool
	PortName    string
	HasChildren bool
	Remaining   []string
}

// VE extracts the interface and port name owned by a VE resource. Administrative
// settings, addresses and protocol bindings remain independently owned children.
func (d *Document) VE(id int64) (VE, error) {
	header, err := d.interfaceHeader("ve " + strconv.FormatInt(id, 10))
	if err != nil {
		return VE{}, err
	}

	state := VE{Exists: header >= 0}
	inside, named := false, false
	for i, command := range d.Commands {
		if command.Parent == -1 {
			inside = i == header
		}
		if i == header {
			continue
		}
		if inside && command.Parent == header && command.kind == portName {
			if named || len(command.Fields) < 2 {
				return VE{}, errors.New("native VE port name is missing or repeated")
			}
			state.PortName = strings.TrimLeft(strings.TrimPrefix(strings.TrimLeft(command.Text, " \t"), "port-name"), " \t")
			named = true
			continue
		}
		if inside {
			state.HasChildren = true
		}
		state.Remaining = append(state.Remaining, command.Text)
	}

	return state, nil
}
