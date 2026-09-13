package dscptrust

import (
	"context"
	"errors"
	"fmt"
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

func validateInterface(name string) error {
	if interfaceid.LAG(name) || strings.HasPrefix(name, "ethernet ") && interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return nil
	}
	return errors.New("interface must be a canonical Ethernet or LAG name")
}

func read(ctx context.Context, device *fastiron.Device, name string) (nativeState, error) {
	if err := validateInterface(name); err != nil {
		return nativeState{}, err
	}
	if _, err := device.Discover(ctx); err != nil {
		return nativeState{}, err
	}
	// Trust defaults are meaningful only after confirming the parent exists.
	var interfaces struct {
		Collection *struct {
			Entries []interfaceEntry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := device.DoREST(ctx, http.MethodGet, "/interfaces", nil, &interfaces); err != nil {
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
			return nativeState{}, errors.New("RESTCONF DSCP trust parent identity is inconsistent")
		}
		if entry.Ethernet != nil && entry.Ethernet.Config != nil && entry.Ethernet.Config.Aggregate != "" {
			return nativeState{}, fmt.Errorf("%s is a LAG member; manage or query DSCP trust configuration on %s", name, entry.Ethernet.Config.Aggregate)
		}
		found = true
	}
	if !found {
		return nativeState{}, fastiron.ErrNotFound
	}
	var response struct {
		Trust *struct {
			Config *struct {
				Enabled *bool `json:"enabled"`
			} `json:"config"`
		} `json:"icx-openconfig-if-trust-dscp-aug:trust-dscp"`
	}
	if err := device.DoREST(ctx, http.MethodGet, endpoint(name), nil, &response); err != nil {
		return nativeState{}, err
	}
	if response.Trust == nil || response.Trust.Config == nil || response.Trust.Config.Enabled == nil {
		return nativeState{}, errors.New("RESTCONF DSCP trust response omitted its configured value")
	}
	// REST configuration can disagree with native state after CLI changes.

	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	return parse(configuration, name)
}

func endpoint(name string) string {
	return path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet", "trust-dscp")
}

func apply(ctx context.Context, device *fastiron.Device, name string, enabled bool) (*bool, error) {
	if err := validateInterface(name); err != nil {
		return nil, err
	}
	unlock, err := device.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := read(ctx, device, name)
	if errors.Is(err, fastiron.ErrNotFound) && !enabled {
		return new(false), device.Persist(ctx)
	}
	if err != nil {
		return nil, err
	}
	// Alignment can require an enable write even when the desired result is disabled.
	if err := current.validate(current.enabled || enabled); err != nil {
		return nil, err
	}
	if current.enabled == enabled {
		return &current.enabled, device.Persist(ctx)
	}

	before := current
	put := func(value bool) error {
		body := map[string]any{"icx-openconfig-if-trust-dscp-aug:trust-dscp": map[string]any{"config": map[string]bool{"enabled": value}}}
		writeErr := device.DoREST(ctx, http.MethodPut, endpoint(name), body, nil)
		configuration, readErr := device.RunningConfig(ctx)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		observed, readErr := parse(configuration, name)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		current = observed
		if !slices.Equal(before.unowned, current.unowned) {
			return errors.Join(writeErr, errors.New("DSCP trust mutation changed unrelated configuration"))
		}
		return writeErr
	}

	// Materialize the native value first so stale metadata cannot skip the desired update.
	if err := put(before.enabled); err != nil {
		return &current.enabled, err
	}
	if current.enabled != before.enabled {
		return &current.enabled, errors.New("DSCP trust alignment changed native configuration")
	}
	if err := put(enabled); err != nil {
		return &current.enabled, err
	}
	if current.enabled != enabled {
		return &current.enabled, errors.New("native DSCP trust did not converge")
	}
	return &current.enabled, device.Persist(ctx)
}
