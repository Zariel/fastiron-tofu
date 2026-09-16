package config

import (
	"errors"
	"strconv"
)

type LAGInterface struct {
	PortName string
	Enabled  bool
}

// LAGInterface reads virtual-interface settings, independently of member settings.
func (d *Document) LAGInterface(id int64) (LAGInterface, error) {
	exists, err := d.HasLAG(id)
	if err != nil {
		return LAGInterface{}, err
	}
	if !exists {
		return LAGInterface{}, errors.New("native LAG does not exist")
	}
	header, err := d.interfaceHeader("lag " + strconv.FormatInt(id, 10))
	if err != nil {
		return LAGInterface{}, err
	}

	state := LAGInterface{Enabled: true}
	named := false
	for _, command := range d.Commands {
		if header < 0 || command.Parent != header {
			continue
		}
		switch command.kind {
		case portName:
			if named || !command.valid {
				return LAGInterface{}, errors.New("native LAG interface name is malformed or repeated")
			}
			state.PortName = command.name
			named = true
		case adminDisable:
			if !state.Enabled || !command.valid || len(command.portRanges) != 0 {
				return LAGInterface{}, errors.New("native LAG interface administrative setting is malformed or repeated")
			}
			state.Enabled = false
		}
	}

	return state, nil
}
