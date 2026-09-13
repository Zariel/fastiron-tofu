package config

import (
	"errors"
)

type IGMP struct {
	Mode      string
	Version   int64
	Remaining []string
}

// VLANs maps native VLAN identities to their command positions.
func (d *Document) VLANs() (map[int64]int, error) {
	vlans := map[int64]int{}
	for i, c := range d.Commands {
		if c.Parent != -1 || c.kind != vlanHeader {
			continue
		}
		if !c.valid {
			return nil, errors.New("native VLAN identity is missing")
		}
		id := c.number
		if id < 1 || id > 4095 {
			return nil, errors.New("native VLAN identity is invalid")
		}
		if _, exists := vlans[id]; exists {
			return nil, errors.New("native configuration repeats a VLAN")
		}
		vlans[id] = i
	}
	return vlans, nil
}

// IGMP returns global settings for scope -1, or overrides for a VLAN header.
func (d *Document) IGMP(scope int) (IGMP, error) {
	var state IGMP
	modeSeen, versionSeen := false, false
	for _, c := range d.Commands {
		if c.Parent != scope || c.kind != multicastConfig || c.global != (scope == -1) {
			state.Remaining = append(state.Remaining, c.Text)
			continue
		}
		if !c.valid {
			return IGMP{}, errors.New("native IGMP setting is malformed")
		}
		switch c.name {
		case "active", "passive", "disable-igmp-snoop":
			if scope == -1 && c.name == "disable-igmp-snoop" {
				state.Remaining = append(state.Remaining, c.Text)
				continue
			}
			if modeSeen {
				return IGMP{}, errors.New("native IGMP mode is ambiguous or unsupported")
			}
			state.Mode, modeSeen = c.name, true
			if state.Mode == "disable-igmp-snoop" {
				state.Mode = "disabled"
			}
		case "version":
			if versionSeen {
				return IGMP{}, errors.New("native IGMP version is ambiguous or unsupported")
			}
			version := c.number
			if version != 2 && version != 3 {
				return IGMP{}, errors.New("native IGMP version is unsupported")
			}
			state.Version, versionSeen = version, true
		default:
			state.Remaining = append(state.Remaining, c.Text)
		}
	}
	if scope == -1 {
		if !modeSeen {
			state.Mode = "disabled"
		}
		if !versionSeen {
			state.Version = 2
		}
	}
	return state, nil
}
