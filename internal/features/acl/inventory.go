package acl

import (
	"cmp"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/config"
)

type aclIdentity struct {
	ID   string `tfsdk:"id"`
	Kind string `tfsdk:"kind"`
	Name string `tfsdk:"name"`
}

func nativeInventory(configuration string) ([]aclIdentity, error) {
	document, err := config.Parse(configuration)
	if err != nil {
		return nil, err
	}
	native, err := document.ACLs()
	if err != nil {
		return nil, err
	}
	identities := make([]aclIdentity, 0, len(native))
	for _, entry := range native {
		identities = append(identities, aclIdentity{ID: entry.ID, Kind: entry.Kind, Name: entry.Name})
	}
	slices.SortFunc(identities, func(a, b aclIdentity) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	return identities, nil
}
