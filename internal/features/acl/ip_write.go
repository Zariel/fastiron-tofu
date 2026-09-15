package acl

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"slices"
	"strconv"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func readIP(ctx context.Context, d *fastiron.Device, family ipFamily, name string) (*ipConfig, error) {
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	current, _, err := ipConfiguration(ctx, d, family, name)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fastiron.ErrNotFound
	}
	return current, nil
}

func ipConfiguration(ctx context.Context, d *fastiron.Device, family ipFamily, name string) (*ipConfig, []string, error) {
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}

	return nativeIP(document, family, name)
}

func applyIP(ctx context.Context, d *fastiron.Device, desired ipConfig) (*ipConfig, error) {
	if err := validateIP(desired); err != nil {
		return nil, err
	}
	if !d.RESTCONFEnabled() {
		return nil, errors.New("IP ACL configuration requires RESTCONF")
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*ipConfig, error) {
		current, unowned, err := ipConfiguration(ctx, d, desired.Family, desired.Name)
		if err != nil {
			return nil, err
		}
		if current != nil && maps.Equal(current.Rules, desired.Rules) {
			return current, nil
		}
		// CLI edits can reach native configuration before RESTCONF's database. A
		// mutation against stale sequences can also rewrite otherwise untouched rules.
		if err := checkIPSequences(ctx, d, desired.Family, desired.Name, current); err != nil {
			return current, err
		}

		// A 404 is also returned when an ACL deletion is refused because it is bound.
		// Native readback, including neighboring ACLs and bindings, determines the result.
		write := func(method, endpoint string, body any, expected map[int64]ipRule) error {
			var writeErr error
			if method == http.MethodDelete {
				writeErr = update.DeleteIfPresent(endpoint)
			} else {
				writeErr = update.REST(method, endpoint, body)
			}
			observed, neighbors, readErr := ipConfiguration(ctx, d, desired.Family, desired.Name)
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			current = observed
			if !slices.Equal(unowned, neighbors) {
				return errors.Join(writeErr, errors.New("ACL operation changed unrelated native configuration"))
			}
			if current == nil || !maps.Equal(current.Rules, expected) {
				return errors.Join(writeErr, errors.New("IP ACL rules did not converge"))
			}
			if writeErr != nil {
				return fmt.Errorf("ACL %s %s: %w", method, endpoint, writeErr)
			}
			return nil
		}
		if current == nil {
			initial := desired.Rules
			// RESTCONF cannot create an empty ACL directly. Create one owned deny
			// rule, then remove it through normal reconciliation before persistence.
			if len(initial) == 0 {
				initial = map[int64]ipRule{1: {Sequence: 1, Action: "deny", Source: "any", Destination: "any"}}
			}
			if err := write(http.MethodPatch, aclPath, ipPayload(ipConfig{Family: desired.Family, Name: desired.Name, Rules: initial}), initial); err != nil {
				return current, err
			}
		}
		// Remove changed sequences before adding replacements, including moves between
		// sequences. RESTCONF rejects adding an identical rule at a second sequence.
		for _, sequence := range slices.Sorted(maps.Keys(current.Rules)) {
			if wanted, ok := desired.Rules[sequence]; ok && wanted == current.Rules[sequence] {
				continue
			}
			expected := maps.Clone(current.Rules)
			delete(expected, sequence)
			endpoint := path.Join(desired.Family.path(desired.Name), "acl-entries", "acl-entry", strconv.FormatInt(sequence, 10))
			if err := write(http.MethodDelete, endpoint, nil, expected); err != nil {
				return current, err
			}
		}
		additions := map[int64]ipRule{}
		for sequence, rule := range desired.Rules {
			if current.Rules[sequence] != rule {
				additions[sequence] = rule
			}
		}
		if len(additions) > 0 {
			if err := write(http.MethodPatch, aclPath, ipPayload(ipConfig{Family: desired.Family, Name: desired.Name, Rules: additions}), desired.Rules); err != nil {
				return current, err
			}
		}
		return current, nil
	})
}

func deleteIP(ctx context.Context, d *fastiron.Device, family ipFamily, name string) error {
	if err := validateIP(ipConfig{Family: family, Name: name}); err != nil {
		return err
	}
	if !d.RESTCONFEnabled() {
		return errors.New("IP ACL configuration requires RESTCONF")
	}
	return d.Update(ctx, func(update *fastiron.Update) error {
		current, unowned, err := ipConfiguration(ctx, d, family, name)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		if referencesACL(unowned, family, name) {
			return errors.New("remove native ACL references before deleting the ACL")
		}
		if err := checkIPSequences(ctx, d, family, name, current); err != nil {
			return err
		}
		writeErr := update.DeleteIfPresent(family.path(name))
		observed, neighbors, readErr := ipConfiguration(ctx, d, family, name)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		if observed != nil {
			return errors.Join(writeErr, errors.New("IP ACL remains in native configuration"))
		}
		if !slices.Equal(unowned, neighbors) {
			return errors.Join(writeErr, errors.New("ACL deletion changed unrelated native configuration"))
		}
		if writeErr != nil {
			return writeErr
		}
		return nil
	})
}
