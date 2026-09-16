package config

import "errors"

func (c Command) resolvePorts(ports map[string][3]uint64) ([]string, error) {
	var names []string
	if c.allPorts {
		for name := range ports {
			names = append(names, name)
		}
		return names, nil
	}
	for _, span := range c.portRanges {
		if span.first[0] != span.last[0] || span.first[1] != span.last[1] || span.first[2] > span.last[2] {
			return nil, errors.New("native port range is invalid or crosses a slot")
		}
		start := len(names)
		for name, id := range ports {
			if id[0] != span.first[0] || id[1] != span.first[1] || id[2] < span.first[2] || id[2] > span.last[2] {
				continue
			}
			names = append(names, name)
		}
		if uint64(len(names)-start) != span.last[2]-span.first[2]+1 {
			return nil, errors.New("interface inventory omits a port referenced by native configuration")
		}
	}
	return names, nil
}
