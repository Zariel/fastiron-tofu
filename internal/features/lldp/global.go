package lldp

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type globalState struct {
	enabled bool
	unowned []string
}

func readGlobalNative(ctx context.Context, device *fastiron.Device) (globalState, error) {
	output, err := device.RunningConfig(ctx)
	if err != nil {
		return globalState{}, err
	}
	document, err := config.Parse(output)
	if err != nil {
		return globalState{}, err
	}
	enabled, unowned, err := document.LLDP()
	return globalState{enabled: enabled, unowned: unowned}, err
}

// The caller holds the device lock through mutation, verification and persistence.
func applyGlobal(ctx context.Context, device *fastiron.Device, enabled bool) (*bool, error) {
	cached, err := readRESTEnabled(ctx, device, "")
	if err != nil {
		return nil, err
	}
	before, err := readGlobalNative(ctx, device)
	if err != nil {
		return nil, err
	}
	if before.enabled == enabled {
		return &before.enabled, device.Persist(ctx)
	}

	targets := []bool{enabled}
	if cached != before.enabled {
		// A cached desired value can suppress the native callback. Synchronize
		// the cache to current native state before applying the desired value.
		targets = []bool{before.enabled, enabled}
	}
	current := before
	for _, target := range targets {
		body := map[string]any{"config": map[string]bool{"enabled": target}}
		writeErr := device.DoREST(ctx, http.MethodPatch, "/lldp/config", body, nil)
		after, readErr := readGlobalNative(ctx, device)
		if readErr != nil {
			return &current.enabled, errors.Join(writeErr, readErr)
		}
		current = after
		// Check both synchronization and desired writes before permitting any
		// subsequent mutation or save of independently owned configuration.
		if !slices.Equal(before.unowned, current.unowned) {
			return &current.enabled, errors.Join(writeErr, errors.New("global LLDP mutation changed unrelated configuration"))
		}
		if current.enabled != target {
			return &current.enabled, errors.Join(writeErr, errors.New("native global LLDP configuration did not converge"))
		}
		if writeErr != nil {
			return &current.enabled, writeErr
		}
		cached, err = readRESTEnabled(ctx, device, "")
		if err != nil {
			return &current.enabled, err
		}
		if cached != current.enabled {
			return &current.enabled, errors.New("RESTCONF global LLDP configuration did not converge")
		}
	}
	return &current.enabled, device.Persist(ctx)
}
