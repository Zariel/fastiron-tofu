package acl

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func readAccessGroup(ctx context.Context, d *fastiron.Device, k accessGroupKey) (accessGroupView, error) {
	if err := k.validate(); err != nil {
		return accessGroupView{}, err
	}
	if !d.RESTCONFEnabled() {
		return accessGroupView{}, errors.New("ACL binding configuration requires RESTCONF")
	}
	if _, err := d.Discover(ctx); err != nil {
		return accessGroupView{}, err
	}
	return accessGroupConfiguration(ctx, d, k)
}

// An empty name removes the owned interface/family/direction slot. Stale REST
// entries are part of that slot, even when they no longer appear in native CLI.
func applyAccessGroup(ctx context.Context, d *fastiron.Device, k accessGroupKey, name string) (*accessGroupView, error) {
	if err := k.validate(); err != nil {
		return nil, err
	}
	if name != "" {
		if err := validateACLName(name); err != nil {
			return nil, err
		}
	}
	if !d.RESTCONFEnabled() {
		return nil, errors.New("ACL binding configuration requires RESTCONF")
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*accessGroupView, error) {
		current, err := accessGroupConfiguration(ctx, d, k)
		if err != nil {
			return nil, err
		}
		if name != "" {
			if err := k.checkParents(current, name); err != nil {
				return nil, err
			}
			if err := checkAccessGroupInterface(ctx, d, k); err != nil {
				return nil, err
			}
		}
		unowned := current.Unowned

		write := func(method, endpoint string, body any, wanted string) error {
			var writeErr error
			if method == http.MethodDelete {
				writeErr = update.DeleteIfPresent(endpoint)
			} else {
				writeErr = update.REST(method, endpoint, body)
			}
			observed, readErr := accessGroupConfiguration(ctx, d, k)
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			current = observed
			if !slices.Equal(unowned, current.Unowned) {
				return errors.Join(writeErr, errors.New("ACL binding operation changed unrelated native configuration"))
			}
			if current.ACL != wanted {
				return errors.Join(writeErr, errors.New("native ACL binding did not converge"))
			}
			return writeErr
		}

		// PATCH replaces the native binding while retaining its old REST key. Prune
		// inactive keys before and after replacement, preserving the active binding.
		prune := func() error {
			for _, stale := range slices.Clone(current.RESTACLs) {
				if stale == current.ACL {
					continue
				}
				if err := write(http.MethodDelete, k.endpoint(stale), nil, current.ACL); err != nil {
					return err
				}
				if slices.Contains(current.RESTACLs, stale) {
					return errors.New("stale RESTCONF ACL binding remains after deletion")
				}
			}
			return nil
		}
		if err := prune(); err != nil {
			return &current, err
		}
		if current.ACL != "" && name == "" {
			previous := current.ACL
			if err := write(http.MethodDelete, k.endpoint(previous), nil, ""); err != nil {
				return &current, err
			}
			if slices.Contains(current.RESTACLs, previous) {
				return &current, errors.New("RESTCONF ACL binding remains after native deletion")
			}
		}
		if name != "" && (current.ACL != name || !slices.Equal(current.RESTACLs, []string{name})) {
			if err := write(http.MethodPatch, "/acl/interfaces", k.payload(name), name); err != nil {
				return &current, err
			}
		}
		if err := prune(); err != nil {
			return &current, err
		}
		expected := []string{}
		if name != "" {
			expected = []string{name}
		}
		if current.ACL != name || !slices.Equal(current.RESTACLs, expected) {
			return &current, errors.New("RESTCONF and native ACL bindings did not converge")
		}
		return &current, nil
	})
}
