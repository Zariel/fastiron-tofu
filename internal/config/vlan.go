package config

import (
	"errors"
	"strconv"
	"strings"
)

func (d *Document) DefaultVLAN() (int64, error) {
	id := int64(1)
	found := false
	for _, command := range d.Commands {
		line := command.Text
		if command.Parent != -1 {
			continue
		}
		if !strings.HasPrefix(line, "default-vlan-id") {
			continue
		}
		fields := strings.Fields(line)
		if found || len(fields) != 2 || fields[0] != "default-vlan-id" {
			return 0, errors.New("invalid or duplicate native default VLAN setting")
		}
		parsed, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || parsed < 1 || parsed > 4095 {
			return 0, errors.New("invalid native default VLAN ID")
		}
		id = parsed
		found = true
	}
	return id, nil
}
