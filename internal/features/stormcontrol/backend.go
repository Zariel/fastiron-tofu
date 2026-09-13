package stormcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type interfaceEntry struct {
	Name   string `json:"name"`
	Config *struct {
		Name string `json:"name"`
	} `json:"config"`
	Ethernet *struct {
		Config *struct {
			Aggregate string `json:"openconfig-if-aggregate:aggregate-id"`
		} `json:"config"`
	} `json:"openconfig-if-ethernet:ethernet"`
}

type limit struct {
	Rate int64 `json:"limit"`
	KBPS bool  `json:"kbps"`
}

func validateInterface(name string) error {
	if interfaceid.LAG(name) || strings.HasPrefix(name, "ethernet ") && interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return nil
	}
	return errors.New("interface must be a canonical Ethernet or LAG name")
}

func (p policy) validate() error {
	if len(p.limits) == 0 {
		if p.unit != "" {
			return errors.New("an empty storm policy has no rate unit")
		}
		return nil
	}
	if p.unit != "pps" && p.unit != "kbps" {
		return errors.New("storm unit must be pps or kbps")
	}
	maximum := int64(8388607)
	if p.unit == "kbps" {
		maximum = 1000000
	}
	for class, rate := range p.limits {
		if !slices.Contains(classes, class) {
			return fmt.Errorf("unknown storm traffic class %q", class)
		}
		if rate < 1 || rate > maximum {
			return fmt.Errorf("%s limit must be between 1 and %d %s", class, maximum, p.unit)
		}
	}
	return nil
}

func endpoint(name string) string {
	return path.Join("/openconfig-interfaces:interfaces", "interface="+url.PathEscape(name), "config", "storm_control_config")
}

func read(ctx context.Context, device *fastiron.Device, name string) (nativeState, error) {
	if err := validateInterface(name); err != nil {
		return nativeState{}, err
	}
	if _, err := device.Discover(ctx); err != nil {
		return nativeState{}, err
	}
	var interfaces struct {
		Collection *struct {
			Entries []interfaceEntry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := device.ReadREST(ctx, "/interfaces", &interfaces); err != nil {
		return nativeState{}, err
	}
	if interfaces.Collection == nil || len(interfaces.Collection.Entries) == 0 {
		return nativeState{}, errors.New("RESTCONF interface collection is missing or empty")
	}
	found := false
	for _, entry := range interfaces.Collection.Entries {
		if entry.Name != name {
			continue
		}
		if found || entry.Config == nil || entry.Config.Name != name {
			return nativeState{}, errors.New("RESTCONF storm-control parent identity is inconsistent")
		}
		if entry.Ethernet != nil && entry.Ethernet.Config != nil && entry.Ethernet.Config.Aggregate != "" {
			return nativeState{}, fmt.Errorf("%s is a LAG member; manage or query storm control on %s", name, entry.Ethernet.Config.Aggregate)
		}
		found = true
	}
	if !found {
		return nativeState{}, fastiron.ErrNotFound
	}
	var response struct {
		Policy json.RawMessage `json:"icx-openconfig-stormcontrol:storm_control_config"`
	}
	if err := device.ReadREST(ctx, endpoint(name), &response); err != nil {
		return nativeState{}, err
	}
	if len(response.Policy) == 0 || string(response.Policy) == "null" {
		return nativeState{}, errors.New("RESTCONF storm-control response omitted its container")
	}
	// REST metadata can omit native settings or retain old rates after CLI changes.
	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	return parse(configuration, name)
}

func apply(ctx context.Context, device *fastiron.Device, name string, desired policy) (*policy, error) {
	if err := validateInterface(name); err != nil {
		return nil, err
	}
	if err := desired.validate(); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, device, func(update *fastiron.Update) (*policy, error) {
		current, err := read(ctx, device, name)
		if errors.Is(err, fastiron.ErrNotFound) && len(desired.limits) == 0 {
			// Parent removal also removes the policy, but a pending save must still finish.
			return &policy{}, nil
		}
		if err != nil {
			return nil, err
		}
		if err := current.writable(); err != nil {
			return nil, err
		}
		if current.policy.equal(desired) {
			return &current.policy, nil
		}

		before := current
		write := func(method, target string, rates map[string]limit) error {
			var body any
			if rates != nil {
				body = map[string]any{"storm_control_config": rates}
			}
			var writeErr error
			if method == http.MethodDelete {
				writeErr = update.DeleteIfPresent(target)
			} else {
				writeErr = update.REST(method, target, body)
			}
			configuration, readErr := device.RunningConfig(ctx)
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			observed, readErr := parse(configuration, name)
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			current = observed
			if current.options || !slices.Equal(before.unowned, current.unowned) {
				return errors.Join(writeErr, errors.New("storm-control mutation changed unrelated configuration or options"))
			}
			return writeErr
		}
		target := endpoint(name)

		// Seed current values before changing or deleting them: an unchanged cache entry
		// can skip the native callback, and native-only settings may lack deletable metadata.
		prime := map[string]limit{}
		for class, rate := range current.policy.limits {
			if desired.unit != current.policy.unit || desired.limits[class] != rate {
				prime[class] = limit{Rate: rate, KBPS: current.policy.unit == "kbps"}
			}
		}
		if len(prime) > 0 {
			if err := write(http.MethodPatch, target, prime); err != nil {
				return &current.policy, err
			}
			if !current.policy.equal(before.policy) {
				return &current.policy, errors.New("storm cache alignment changed native policy")
			}
		}

		if len(current.policy.limits) > 0 && current.policy.unit != desired.unit {
			// FastIron requires one mode across all classes; changing it needs a policy reset.
			if err := write(http.MethodDelete, target, nil); err != nil {
				return &current.policy, err
			}
			if len(current.policy.limits) != 0 {
				return &current.policy, errors.New("native storm policy did not clear before the unit change")
			}
		}
		for _, class := range classes {
			if _, exists := current.policy.limits[class]; !exists {
				continue
			}
			if _, keep := desired.limits[class]; keep {
				continue
			}
			expected := policy{unit: current.policy.unit, limits: maps.Clone(current.policy.limits)}
			delete(expected.limits, class)
			if len(expected.limits) == 0 {
				expected.unit = ""
			}
			if err := write(http.MethodDelete, path.Join(target, class), nil); err != nil {
				return &current.policy, err
			}
			if !current.policy.equal(expected) {
				return &current.policy, errors.New("storm class deletion changed unexpected limits")
			}
		}
		changed := map[string]limit{}
		for class, rate := range desired.limits {
			if _, exists := current.policy.limits[class]; !exists {
				// A CLI removal can leave cached rates that suppress recreation callbacks.
				expected := current.policy
				if err := write(http.MethodDelete, path.Join(target, class), nil); err != nil {
					return &current.policy, err
				}
				if !current.policy.equal(expected) {
					return &current.policy, errors.New("storm cache invalidation changed native policy")
				}
			}
			if current.policy.unit != desired.unit || current.policy.limits[class] != rate {
				changed[class] = limit{Rate: rate, KBPS: desired.unit == "kbps"}
			}
		}
		if len(changed) > 0 {
			if err := write(http.MethodPatch, target, changed); err != nil {
				return &current.policy, err
			}
		}
		if !current.policy.equal(desired) {
			return &current.policy, errors.New("native storm policy did not converge")
		}
		return &current.policy, nil
	})
}
