package config

import (
	"errors"
	"strings"
)

type MEDPolicy struct {
	Traffic  string
	VLAN     int64
	Priority int64
	DSCP     int64
}

// MEDPolicies returns policies keyed by Ethernet interface and application, plus
// unowned commands. Port ranges are resolved against complete Ethernet inventory.
func (d *Document) MEDPolicies(names []string) (map[string]map[string]MEDPolicy, []string, error) {
	ports := make(map[string][3]uint64, len(names))
	for _, name := range names {
		raw, canonical := strings.CutPrefix(name, "ethernet ")
		id, valid := parseEthernetPort(raw)
		if !canonical || !valid {
			return nil, nil, errors.New("LLDP-MED inventory contains an invalid Ethernet identity")
		}
		if _, exists := ports[name]; exists {
			return nil, nil, errors.New("LLDP-MED inventory repeats an Ethernet identity")
		}
		ports[name] = id
	}
	policies := map[string]map[string]MEDPolicy{}
	var remaining []string
	for _, command := range d.Commands {
		if command.Parent != -1 || command.kind != lldpMED {
			remaining = append(remaining, command.Text)
			continue
		}
		policy := command.med
		if !command.valid || policy.DSCP > 63 || policy.Priority > 7 || (policy.Traffic == "tagged" && (policy.VLAN < 1 || policy.VLAN > 4094)) {
			return nil, nil, errors.New("native LLDP-MED network policy is malformed")
		}
		members, err := command.resolvePorts(ports)
		if err != nil {
			return nil, nil, err
		}
		for _, name := range members {
			if policies[name] == nil {
				policies[name] = map[string]MEDPolicy{}
			}
			if _, exists := policies[name][command.name]; exists {
				return nil, nil, errors.New("native LLDP-MED application is repeated on a port")
			}
			policies[name][command.name] = policy
		}
	}
	return policies, remaining, nil
}
