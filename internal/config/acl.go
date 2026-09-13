package config

import (
	"errors"
	"fmt"
)

type ACL struct{ ID, Kind, Name string }

func (d *Document) ACLs() ([]ACL, error) {
	identities := []ACL{}
	seen := map[ACL]bool{}
	for _, command := range d.Commands {
		line := command.Text
		if command.Parent != -1 {
			continue
		}
		if command.kind != aclHeader {
			continue
		}
		if !command.valid {
			return nil, errors.New("unrecognized native ACL header")
		}
		identity := ACL{Kind: command.family, Name: command.name}
		if seen[identity] {
			return nil, fmt.Errorf("duplicate native ACL identity: %s", line)
		}
		seen[identity] = true
		identity.ID = line
		identities = append(identities, identity)
	}
	return identities, nil
}
