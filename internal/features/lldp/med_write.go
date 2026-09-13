package lldp

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type medWriteResult struct {
	policy    *config.MEDPolicy
	verified  bool
	attempted bool
}

func applyMED(ctx context.Context, device *fastiron.Device, name, application string, desired *config.MEDPolicy) (medWriteResult, error) {
	if err := validateInterface(name); err != nil {
		return medWriteResult{}, err
	}
	if !validMEDApplication(application) {
		return medWriteResult{}, errors.New("invalid MED application")
	}
	if desired != nil {
		if err := validateMEDPolicy(*desired); err != nil {
			return medWriteResult{}, err
		}
	}
	unlock, err := device.Lock(ctx)
	if err != nil {
		return medWriteResult{}, err
	}
	defer unlock()
	if _, err := device.Discover(ctx); err != nil {
		return medWriteResult{}, err
	}
	inventory, err := readRESTInterfaces(ctx, device)
	if err != nil {
		return medWriteResult{}, err
	}
	if _, exists := inventory[name]; !exists {
		return medWriteResult{}, errors.New("LLDP inventory omits the requested MED interface")
	}
	cached, err := readMEDCache(ctx, device)
	if err != nil {
		return medWriteResult{}, err
	}
	before, unowned, err := readMEDNative(ctx, device)
	if err != nil {
		return medWriteResult{}, err
	}
	current := medWriteResult{verified: true}
	if p, exists := before[name][application]; exists {
		current.policy = &p
	}
	if desired != nil && current.policy != nil && *desired == *current.policy {
		return current, device.Persist(ctx)
	}
	others := withoutMED(before, name, application)

	// The lock covers every native observation through persistence. A mutation's
	// HTTP result alone cannot prove convergence or preservation on FastIron.
	mutate := func(method, path string, body any) error {
		current.attempted = true
		writeErr := device.DoREST(ctx, method, path, body, nil)
		after, remaining, readErr := readMEDNative(ctx, device)
		if readErr != nil {
			current.verified = false
			return errors.Join(writeErr, readErr)
		}
		current = medWriteResult{verified: true, attempted: true}
		if p, exists := after[name][application]; exists {
			current.policy = &p
		}
		if !slices.Equal(unowned, remaining) || !reflect.DeepEqual(others, withoutMED(after, name, application)) {
			return errors.Join(writeErr, errors.New("MED mutation changed unrelated configuration"))
		}
		return writeErr
	}
	targets := medTargets(cached, name, application)
	if len(targets) == 0 && current.policy != nil {
		native := *current.policy
		if err := mutate(http.MethodPatch, "/lldp/med", medPayload(name, application, native)); err != nil {
			return current, err
		}
		if current.policy == nil || *current.policy != native {
			return current, errors.New("MED cache synchronization changed native policy")
		}
		cached, err = readMEDCache(ctx, device)
		if err != nil {
			return current, err
		}
		targets = medTargets(cached, name, application)
		if len(targets) == 0 {
			return current, errors.New("MED cache synchronization did not create a port attachment")
		}
	}
	// Stale port references can delete the current native policy, regardless of
	// their cached values. Remove all owned references before creating its successor.
	for _, attachment := range targets {
		if !slices.Contains(cached, attachment) {
			continue
		}
		if err := mutate(http.MethodDelete, attachment.path(), nil); err != nil {
			return current, err
		}
		cached, err = readMEDCache(ctx, device)
		if err != nil {
			return current, err
		}
		if slices.Contains(cached, attachment) {
			return current, errors.New("MED port deletion did not converge in RESTCONF")
		}
	}
	if current.policy != nil {
		return current, errors.New("native MED policy remains after removing cached port attachments")
	}
	cached, err = readMEDCache(ctx, device)
	if err != nil {
		return current, err
	}
	if len(medTargets(cached, name, application)) != 0 {
		return current, errors.New("MED cache still references the owned application and port")
	}
	if desired == nil {
		return current, device.Persist(ctx)
	}

	if err := mutate(http.MethodPatch, "/lldp/med", medPayload(name, application, *desired)); err != nil {
		return current, err
	}
	if current.policy == nil || *current.policy != *desired {
		return current, errors.New("native MED policy did not converge to the requested tagging and priority values")
	}
	cached, err = readMEDCache(ctx, device)
	if err != nil {
		return current, err
	}
	expected := medAttachment{Interface: name, Application: application, Policy: *desired}
	targets = medTargets(cached, name, application)
	if len(targets) != 1 || targets[0] != expected {
		return current, errors.New("MED cache did not converge to the requested port policy")
	}
	return current, device.Persist(ctx)
}

func validateMEDPolicy(p config.MEDPolicy) error {
	if p.DSCP < 0 || p.DSCP > 63 || p.Priority < 0 || p.Priority > 7 {
		return errors.New("MED DSCP or priority is outside its valid range")
	}
	switch p.Traffic {
	case "tagged":
		if p.VLAN < 1 || p.VLAN > 4094 {
			return errors.New("tagged MED policy requires VLAN 1 through 4094")
		}
	case "priority-tagged":
		if p.VLAN != 0 {
			return errors.New("priority-tagged MED policy cannot specify a VLAN")
		}
	case "untagged":
		if p.VLAN != 0 || p.Priority != 0 {
			return errors.New("untagged MED policy cannot specify a VLAN or priority")
		}
	default:
		return errors.New("invalid MED traffic mode")
	}
	return nil
}

func medTargets(cached []medAttachment, name, application string) []medAttachment {
	var targets []medAttachment
	for _, a := range cached {
		if a.Interface == name && a.Application == application {
			targets = append(targets, a)
		}
	}
	return targets
}

func withoutMED(policies map[string]map[string]config.MEDPolicy, name, application string) map[string]map[string]config.MEDPolicy {
	others := maps.Clone(policies)
	if policies[name] != nil {
		others[name] = maps.Clone(policies[name])
		delete(others[name], application)
		if len(others[name]) == 0 {
			delete(others, name)
		}
	}
	return others
}

func medPayload(name, application string, p config.MEDPolicy) map[string]any {
	entry := map[string]any{"dscp": p.DSCP, "ports": []string{name}}
	if p.Traffic != "untagged" {
		entry["priority"] = p.Priority
	}
	if p.Traffic == "tagged" {
		entry["vlan"] = p.VLAN
	}
	return map[string]any{"icx-openconfig-lldp-aug:med": map[string]any{"network-policy": []any{map[string]any{"application": application, "traffic": p.Traffic, p.Traffic: []any{entry}}}}}
}
