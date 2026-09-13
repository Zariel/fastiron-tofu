package acl

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

const aclPath = "/acl/acl-sets"

func standardPath(name string) string {
	return path.Join(aclPath, "acl-set", url.PathEscape(name), "ACL_IPV4")
}

func standardPayload(name string, rules map[int64]standardRule) map[string]any {
	entries := []any{}
	for _, sequence := range slices.Sorted(maps.Keys(rules)) {
		rule := rules[sequence]
		source := rule.Source
		if source == "any" {
			source = "0.0.0.0/0"
		}
		action := "ACCEPT"
		if rule.Action == "deny" {
			action = "DROP"
		}
		entries = append(entries, map[string]any{
			"sequence-id": sequence, "config": map[string]any{"sequence-id": sequence},
			"ipv4":    map[string]any{"config": map[string]any{"source-address": source}},
			"actions": map[string]any{"config": map[string]string{"forwarding-action": action}},
		})
	}
	return map[string]any{"acl-sets": map[string]any{"acl-set": []any{map[string]any{
		"name": name, "type": "ACL_IPV4", "standard": true,
		"config":      map[string]any{"name": name, "type": "ACL_IPV4", "standard": true},
		"acl-entries": map[string]any{"acl-entry": entries},
	}}}}
}

func standardConfiguration(ctx context.Context, d *fastiron.Device, name string) (*standardConfig, []string, error) {
	output, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	output, err = fastiron.NormalizeConfiguration(output)
	if err != nil {
		return nil, nil, err
	}
	return nativeStandard(output, name)
}

func applyStandard(ctx context.Context, d *fastiron.Device, desired standardConfig) (*standardConfig, error) {
	if err := validateStandard(desired); err != nil {
		return nil, err
	}
	if !d.RESTCONFEnabled() {
		return nil, errors.New("standard ACL configuration requires RESTCONF")
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*standardConfig, error) {
		current, unowned, err := standardConfiguration(ctx, d, desired.Name)
		if err != nil {
			return nil, err
		}

		// A 404 is also returned when an ACL deletion is refused because it is bound.
		// Native readback, including neighboring ACLs and bindings, determines the result.
		write := func(method, endpoint string, body any, expected map[int64]standardRule) error {
			var writeErr error
			if method == http.MethodDelete {
				writeErr = update.DeleteIfPresent(endpoint)
			} else {
				writeErr = update.REST(method, endpoint, body)
			}
			observed, neighbors, readErr := standardConfiguration(ctx, d, desired.Name)
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			current = observed
			if !slices.Equal(unowned, neighbors) {
				return errors.Join(writeErr, errors.New("ACL operation changed unrelated native configuration"))
			}
			if current == nil || !maps.Equal(current.Rules, expected) {
				return errors.Join(writeErr, errors.New("standard ACL rules did not converge"))
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
				initial = map[int64]standardRule{1: {Sequence: 1, Action: "deny", Source: "any"}}
			}
			if err := write(http.MethodPatch, aclPath, standardPayload(desired.Name, initial), initial); err != nil {
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
			endpoint := path.Join(standardPath(desired.Name), "acl-entries", "acl-entry", strconv.FormatInt(sequence, 10))
			if err := write(http.MethodDelete, endpoint, nil, expected); err != nil {
				return current, err
			}
		}
		additions := map[int64]standardRule{}
		for sequence, rule := range desired.Rules {
			if current.Rules[sequence] != rule {
				additions[sequence] = rule
			}
		}
		if len(additions) > 0 {
			if err := write(http.MethodPatch, aclPath, standardPayload(desired.Name, additions), desired.Rules); err != nil {
				return current, err
			}
		}
		return current, nil
	})
}

func deleteStandard(ctx context.Context, d *fastiron.Device, name string) error {
	if err := validateStandard(standardConfig{Name: name}); err != nil {
		return err
	}
	if !d.RESTCONFEnabled() {
		return errors.New("standard ACL configuration requires RESTCONF")
	}
	return d.Update(ctx, func(update *fastiron.Update) error {
		current, unowned, err := standardConfiguration(ctx, d, name)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		if referencesACL(unowned, ipv4ACL, name) {
			return errors.New("remove native ACL references before deleting the ACL")
		}
		writeErr := update.DeleteIfPresent(standardPath(name))
		observed, neighbors, readErr := standardConfiguration(ctx, d, name)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		if observed != nil {
			return errors.Join(writeErr, errors.New("standard ACL remains in native configuration"))
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
