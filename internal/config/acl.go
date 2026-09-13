package config

import (
	"errors"
	"fmt"
	"strings"
)

type ACL struct{ ID, Kind, Name string }

func (d *Document) ACLs() ([]ACL, error) {
	identities := []ACL{}
	seen := map[string]bool{}
	for _, command := range d.Commands {
		line := command.Text
		if command.Parent != -1 {
			continue
		}
		var kind, name string
		switch {
		case strings.HasPrefix(line, "ip access-list standard "):
			kind, name = "ipv4_standard", strings.TrimPrefix(line, "ip access-list standard ")
		case strings.HasPrefix(line, "ip access-list extended "):
			kind, name = "ipv4_extended", strings.TrimPrefix(line, "ip access-list extended ")
		case strings.HasPrefix(line, "ipv6 access-list "):
			kind, name = "ipv6", strings.TrimPrefix(line, "ipv6 access-list ")
		case strings.HasPrefix(line, "mac access-list "):
			kind, name = "mac", strings.TrimPrefix(line, "mac access-list ")
		case strings.HasPrefix(line, "ip access-list"), strings.HasPrefix(line, "ipv6 access-list"), strings.HasPrefix(line, "mac access-list"):
			return nil, errors.New("unrecognized native ACL header")
		default:
			continue
		}
		if len(strings.Fields(name)) != 1 || strings.TrimSpace(name) != name {
			return nil, errors.New("invalid native ACL name")
		}
		if seen[line] {
			return nil, fmt.Errorf("duplicate native ACL identity: %s", line)
		}
		seen[line] = true
		identities = append(identities, ACL{ID: line, Kind: kind, Name: name})
	}
	return identities, nil
}
