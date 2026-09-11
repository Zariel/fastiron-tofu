package aaa

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type account struct {
	Username  string
	Privilege int64
}

func readUsers(ctx context.Context, d *fastiron.Device) ([]account, error) {
	if !d.RESTCONFEnabled() {
		return nil, errors.New("local user discovery currently requires RESTCONF")
	}
	var response struct {
		Users *struct {
			User []struct {
				Username string `json:"username"`
				Config   *struct {
					Username  string `json:"username"`
					Privilege *int64 `json:"icx-openconfig-aaa-aug:privilege"`
				} `json:"config"`
			} `json:"user"`
		} `json:"openconfig-system:users"`
	}
	if err := d.DoREST(ctx, http.MethodGet, "/system/aaa/authentication/users", nil, &response); err != nil {
		return nil, err
	}
	if response.Users == nil {
		return nil, errors.New("RESTCONF user response is missing its user container")
	}
	users := []account{}
	names := map[string]bool{}
	for _, entry := range response.Users.User {
		if entry.Username == "" || names[entry.Username] || entry.Config == nil || entry.Config.Username != entry.Username {
			return nil, errors.New("RESTCONF user response has an inconsistent or duplicate identity")
		}
		if entry.Config.Privilege == nil {
			return nil, errors.New("RESTCONF user response is missing its privilege")
		}
		names[entry.Username] = true
		users = append(users, account{Username: entry.Username, Privilege: *entry.Config.Privilege})
	}
	slices.SortFunc(users, func(a, b account) int { return strings.Compare(a.Username, b.Username) })
	return users, nil
}
