package config

import (
	"errors"
)

func (d *Document) DefaultVLAN() (int64, error) {
	id := int64(1)
	found := false
	for _, command := range d.Commands {
		if command.Parent != -1 {
			continue
		}
		if command.kind != defaultVLAN {
			continue
		}
		if found || !command.valid {
			return 0, errors.New("invalid or duplicate native default VLAN setting")
		}
		parsed := command.number
		if parsed < 1 || parsed > 4095 {
			return 0, errors.New("invalid native default VLAN ID")
		}
		id = parsed
		found = true
	}
	return id, nil
}
