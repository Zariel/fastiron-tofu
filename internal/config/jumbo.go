package config

import "errors"

// Jumbo returns configured global jumbo support and every unowned command.
func (d *Document) Jumbo() (bool, []string, error) {
	enabled := false
	var remaining []string
	for _, command := range d.Commands {
		if command.Parent != -1 || command.kind != jumboMode {
			remaining = append(remaining, command.Text)
			continue
		}
		if enabled || len(command.Fields) != 1 {
			return false, nil, errors.New("native jumbo configuration is malformed or repeated")
		}
		enabled = true
	}
	return enabled, remaining, nil
}
