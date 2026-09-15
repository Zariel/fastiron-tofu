package config

import "errors"

// HasLAG checks the native aggregate definition, independently of interface policy stanzas.
func (d *Document) HasLAG(id int64) (bool, error) {
	if id < 1 {
		return false, errors.New("invalid LAG identity")
	}
	found := false
	for _, command := range d.Commands {
		if command.Parent != -1 || command.kind != lagHeader {
			continue
		}
		if !command.valid || command.number < 1 {
			return false, errors.New("native LAG header is malformed")
		}
		if command.number != id {
			continue
		}
		if found {
			return false, errors.New("native LAG identity is repeated")
		}
		found = true
	}
	return found, nil
}
