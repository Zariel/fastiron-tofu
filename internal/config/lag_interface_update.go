package config

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

// CheckLAGInterfaceUpdate verifies that only virtual-interface fields changed.
// Native administrative transitions also enable or disable every member.
func (d *Document) CheckLAGInterfaceUpdate(before *Document, id int64, ports []string) error {
	previous, err := before.LAGInterface(id)
	if err != nil {
		return err
	}
	current, err := d.LAGInterface(id)
	if err != nil {
		return err
	}
	lags, err := before.LAGs(ports)
	if err != nil {
		return err
	}
	var members []string
	for _, lag := range lags {
		if lag.ID == id {
			members = lag.Members
			break
		}
	}
	inventory := map[string][3]uint64{}
	for _, name := range members {
		port, ok := parseEthernetPort(strings.TrimPrefix(name, "ethernet "))
		if !ok {
			return errors.New("invalid native LAG member")
		}
		inventory[name] = port
	}

	administrative := previous.Enabled != current.Enabled
	remaining, _, err := before.lagInterfaceRemaining(id, inventory, administrative)
	if err != nil {
		return err
	}
	observed, disabled, err := d.lagInterfaceRemaining(id, inventory, administrative)
	if err != nil {
		return err
	}
	if !slices.Equal(remaining, observed) {
		return errors.New("LAG interface operation changed unrelated native configuration")
	}
	if !administrative {
		return nil
	}
	if current.Enabled && len(disabled) != 0 {
		return errors.New("LAG enable operation left disabled members")
	}
	if !current.Enabled && !slices.Equal(disabled, members) {
		return errors.New("LAG disable operation did not disable every member")
	}
	return nil
}

func (d *Document) lagInterfaceRemaining(id int64, members map[string][3]uint64, administrative bool) ([]string, []string, error) {
	virtual, err := d.interfaceHeader("lag " + strconv.FormatInt(id, 10))
	if err != nil {
		return nil, nil, err
	}
	header := -1
	for i, command := range d.Commands {
		if command.Parent == -1 && command.kind == lagHeader && command.number == id {
			header = i
			break
		}
	}
	var remaining, disabled []string
	seen := map[string]bool{}
	for i, command := range d.Commands {
		if i == virtual {
			continue
		}
		if virtual >= 0 && command.Parent == virtual && (command.kind == portName || command.kind == adminDisable) {
			continue
		}
		if !administrative || command.Parent != header || command.kind != adminDisable {
			remaining = append(remaining, command.Text)
			continue
		}
		if !command.valid || len(command.portRanges) == 0 {
			return nil, nil, errors.New("native LAG member administrative setting is malformed")
		}
		names, err := command.resolvePorts(members)
		if err != nil {
			return nil, nil, err
		}
		for _, name := range names {
			if seen[name] {
				return nil, nil, errors.New("native LAG member administrative setting is repeated")
			}
			seen[name] = true
			disabled = append(disabled, name)
		}
	}
	slices.Sort(disabled)
	return remaining, disabled, nil
}
