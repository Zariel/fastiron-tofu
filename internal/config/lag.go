package config

import (
	"cmp"
	"errors"
	"slices"
	"strings"
)

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

// LAG describes a native aggregate definition and its complete member set.
type LAG struct {
	ID         int64
	Name, Mode string
	Members    []string
}

// LAGs resolves native members against the physical inventory so incomplete
// interface responses cannot silently truncate ranges or change ownership.
func (d *Document) LAGs(names []string) ([]LAG, error) {
	ports := make(map[string][3]uint64, len(names))
	for _, name := range names {
		raw, canonical := strings.CutPrefix(name, "ethernet ")
		id, valid := parseEthernetPort(raw)
		if !canonical || !valid {
			return nil, errors.New("LAG inventory contains an invalid Ethernet identity")
		}
		if _, exists := ports[name]; exists {
			return nil, errors.New("LAG inventory repeats an Ethernet identity")
		}
		ports[name] = id
	}
	lags := []LAG{}
	ids, labels, members := map[int64]bool{}, map[string]bool{}, map[string]bool{}
	for i, command := range d.Commands {
		if command.Parent != -1 || command.kind != lagHeader {
			continue
		}
		if !command.valid || command.number < 1 {
			return nil, errors.New("native LAG header is malformed")
		}
		if ids[command.number] || labels[command.name] {
			return nil, errors.New("native LAG repeats an identity or name")
		}
		ids[command.number], labels[command.name] = true, true
		lag := LAG{ID: command.number, Name: command.name, Mode: command.family, Members: []string{}}
		seen := false
		for _, child := range d.Commands {
			if child.Parent != i || child.kind != lagPorts {
				continue
			}
			if seen || !child.valid {
				return nil, errors.New("native LAG members are malformed or repeated")
			}
			seen = true
			resolved, err := child.resolvePorts(ports)
			if err != nil {
				return nil, err
			}
			for _, name := range resolved {
				if members[name] {
					return nil, errors.New("native LAG member is assigned more than once")
				}
				members[name] = true
			}
			lag.Members = resolved
		}
		slices.Sort(lag.Members)
		lags = append(lags, lag)
	}
	slices.SortFunc(lags, func(a, b LAG) int { return cmp.Compare(a.ID, b.ID) })
	return lags, nil
}
