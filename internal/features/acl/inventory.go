package acl

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type aclIdentity struct {
	ID   string `tfsdk:"id"`
	Kind string `tfsdk:"kind"`
	Name string `tfsdk:"name"`
}

func nativeInventory(configuration string) ([]aclIdentity, error) {
	identities := []aclIdentity{}
	seen := map[string]bool{}
	for _, line := range strings.Split(configuration, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
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
		identities = append(identities, aclIdentity{ID: line, Kind: kind, Name: name})
	}
	slices.SortFunc(identities, func(a, b aclIdentity) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	return identities, nil
}
