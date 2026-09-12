package acl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func readMAC(ctx context.Context, device *fastiron.Device, name string) (*macConfig, error) {
	if _, err := device.Discover(ctx); err != nil {
		return nil, err
	}
	current, _, err := macConfiguration(ctx, device, name)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fastiron.ErrNotFound
	}
	return current, nil
}

func macConfiguration(ctx context.Context, device *fastiron.Device, name string) (*macConfig, []string, error) {
	output, err := device.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	output, err = fastiron.NormalizeConfiguration(output)
	if err != nil {
		return nil, nil, err
	}
	return nativeMAC(output, name)
}

func applyMAC(ctx context.Context, device *fastiron.Device, desired macConfig) (*macConfig, error) {
	if err := validateMAC(desired); err != nil {
		return nil, err
	}
	if len(desired.Rules) > 65000 {
		return nil, errors.New("MAC ACL exceeds the RESTCONF entry ID space")
	}
	if !device.RESTCONFEnabled() {
		return nil, errors.New("MAC ACL configuration requires RESTCONF")
	}
	unlock, err := device.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := device.Discover(ctx); err != nil {
		return nil, err
	}
	current, unowned, err := macConfiguration(ctx, device, desired.Name)
	if err != nil {
		return nil, err
	}
	if current != nil && slices.Equal(current.Rules, desired.Rules) {
		return current, device.Persist(ctx)
	}
	entries, err := macEntries(ctx, device, desired.Name, current)
	if err != nil {
		return current, err
	}
	target := macTargets(entries, desired.Rules)
	wanted := make(map[macEntry]bool, len(target))
	for _, entry := range target {
		wanted[entry] = true
	}

	write := func(method, endpoint string, body any, expected []macEntry) error {
		// A reboot or CLI edit can invalidate IDs between mutations. Never use
		// an ID unless it still identifies the rule observed at this step.
		observedEntries, err := macEntries(ctx, device, desired.Name, current)
		if err != nil {
			return err
		}
		if !slices.Equal(observedEntries, entries) {
			return errors.New("MAC ACL REST entry IDs changed during reconciliation; retry")
		}
		writeErr := device.DoREST(ctx, method, endpoint, body, nil)
		if method == http.MethodDelete && errors.Is(writeErr, restconf.ErrNotFound) {
			writeErr = nil
		}
		observed, neighbors, readErr := macConfiguration(ctx, device, desired.Name)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		current = observed
		if !slices.Equal(unowned, neighbors) {
			return errors.Join(writeErr, errors.New("MAC ACL operation changed unrelated native configuration"))
		}
		rules := make([]macRule, 0, len(expected))
		for _, entry := range expected {
			rules = append(rules, entry.Rule)
		}
		if current == nil || !slices.Equal(current.Rules, rules) {
			return errors.Join(writeErr, errors.New("MAC ACL rules did not converge"))
		}
		if writeErr != nil {
			return fmt.Errorf("MAC ACL %s %s: %w", method, endpoint, writeErr)
		}
		entries = expected
		return nil
	}
	if current == nil {
		initial := target
		// Empty ACL creation needs a temporary owned rule; remove it before saving.
		if len(initial) == 0 {
			initial = []macEntry{{ID: 1, Rule: macRule{Action: "deny"}}}
		}
		if err := write(http.MethodPatch, aclPath, macPayload(desired.Name, initial), initial); err != nil {
			return current, err
		}
	}
	// Remove changed entries before additions, so moves cannot collide with
	// duplicate packet rules. Unchanged entries retain their current IDs.
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if wanted[entry] {
			continue
		}
		expected := slices.Delete(slices.Clone(entries), index, index+1)
		endpoint := path.Join(macPath(desired.Name), "acl-entries", "acl-entry", strconv.FormatInt(entry.ID, 10))
		if err := write(http.MethodDelete, endpoint, nil, expected); err != nil {
			return current, err
		}
	}
	retained := make(map[macEntry]bool, len(entries))
	for _, entry := range entries {
		retained[entry] = true
	}
	var additions []macEntry
	for _, entry := range target {
		if !retained[entry] {
			additions = append(additions, entry)
		}
	}
	if len(additions) > 0 {
		if err := write(http.MethodPatch, aclPath, macPayload(desired.Name, additions), target); err != nil {
			return current, err
		}
	}
	return current, device.Persist(ctx)
}

func macTargets(current []macEntry, rules []macRule) []macEntry {
	// Reuse native positions without persisting their runtime IDs. If appending
	// would exhaust the ID space, derive a fresh compact numbering for this write.
	rekey := len(current) > 0 && int64(len(rules)-len(current))+current[len(current)-1].ID > 65000
	target := make([]macEntry, 0, len(rules))
	var last int64
	for index, rule := range rules {
		last++
		if !rekey && index < len(current) {
			last = current[index].ID
		}
		target = append(target, macEntry{ID: last, Rule: rule})
	}
	return target
}

func deleteMAC(ctx context.Context, device *fastiron.Device, name string) error {
	if err := validateACLName(name); err != nil {
		return err
	}
	if !device.RESTCONFEnabled() {
		return errors.New("MAC ACL configuration requires RESTCONF")
	}
	unlock, err := device.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := device.Discover(ctx); err != nil {
		return err
	}
	current, unowned, err := macConfiguration(ctx, device, name)
	if err != nil {
		return err
	}
	if current == nil {
		return device.Persist(ctx)
	}
	for _, line := range unowned {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "mac" && fields[1] == "access-group" && fields[2] == name {
			return errors.New("remove native ACL references before deleting the ACL")
		}
	}
	if _, err := macEntries(ctx, device, name, current); err != nil {
		return err
	}
	writeErr := device.DoREST(ctx, http.MethodDelete, macPath(name), nil, nil)
	if errors.Is(writeErr, restconf.ErrNotFound) {
		writeErr = nil
	}
	observed, neighbors, readErr := macConfiguration(ctx, device, name)
	if readErr != nil {
		return errors.Join(writeErr, readErr)
	}
	if observed != nil {
		return errors.Join(writeErr, errors.New("MAC ACL remains in native configuration"))
	}
	if !slices.Equal(unowned, neighbors) {
		return errors.Join(writeErr, errors.New("MAC ACL deletion changed unrelated native configuration"))
	}
	if writeErr != nil {
		return writeErr
	}
	return device.Persist(ctx)
}
