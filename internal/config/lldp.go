package config

import "errors"

// LLDP returns global enable configuration and preserves independently owned
// port modes, advertisements and LLDP-MED configuration.
func (d *Document) LLDP() (bool, []string, error) {
	enabled, seen := true, false
	var remaining []string
	for _, command := range d.Commands {
		if command.Parent != -1 || command.kind != lldpRun {
			remaining = append(remaining, command.Text)
			continue
		}
		if seen || !command.valid {
			return false, nil, errors.New("native global LLDP configuration is malformed or repeated")
		}
		enabled, seen = !command.negated, true
	}
	return enabled, remaining, nil
}
