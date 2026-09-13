package config

import (
	"errors"
	"strconv"
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
		if c.Parent != -1 || c.Fields[0] != "vlan" {
			continue
		}
		if len(c.Fields) < 2 {
			return nil, errors.New("native VLAN identity is missing")
		}
		id, err := strconv.ParseInt(c.Fields[1], 10, 64)
		if err != nil || id < 1 || id > 4095 {
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
		fields := c.Fields
		if scope == -1 && len(fields) > 0 && fields[0] == "ip" {
			fields = fields[1:]
		}
		if c.Parent != scope || len(fields) == 0 || fields[0] != "multicast" || (scope == -1 && c.Fields[0] != "ip") {
			state.Remaining = append(state.Remaining, c.Text)
			continue
		}
		if len(fields) < 2 {
			return IGMP{}, errors.New("native IGMP mode is missing")
		}
		switch fields[1] {
		case "active", "passive", "disable-igmp-snoop":
			if scope == -1 && fields[1] == "disable-igmp-snoop" {
				state.Remaining = append(state.Remaining, c.Text)
				continue
			}
			if len(fields) != 2 || modeSeen {
				return IGMP{}, errors.New("native IGMP mode is ambiguous or unsupported")
			}
			state.Mode, modeSeen = fields[1], true
			if state.Mode == "disable-igmp-snoop" {
				state.Mode = "disabled"
			}
		case "version":
			if len(fields) != 3 || versionSeen {
				return IGMP{}, errors.New("native IGMP version is ambiguous or unsupported")
			}
			version, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil || (version != 2 && version != 3) {
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
