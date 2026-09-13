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
	cached, err := readEnabled(ctx, device, "")
	if err != nil {
		return nil, err
	}
	before, err := readGlobalNative(ctx, device)
	if err != nil {
		return nil, err
	}
	if cached != before.enabled {
		return &before.enabled, errors.New("RESTCONF global LLDP configuration disagrees with native configuration")
	}
	if before.enabled == enabled {
		return &before.enabled, device.Persist(ctx)
	}

	body := map[string]any{"config": map[string]bool{"enabled": enabled}}
	writeErr := device.DoREST(ctx, http.MethodPatch, "/lldp/config", body, nil)
	after, readErr := readGlobalNative(ctx, device)
	if readErr != nil {
		return &before.enabled, errors.Join(writeErr, readErr)
	}
	// Saving is allowed only after native convergence and preservation of every
	// unowned command, including per-port LLDP modes and advertisements.
	if !slices.Equal(before.unowned, after.unowned) {
		return &after.enabled, errors.Join(writeErr, errors.New("global LLDP mutation changed unrelated configuration"))
	}
	if after.enabled != enabled {
		return &after.enabled, errors.Join(writeErr, errors.New("native global LLDP configuration did not converge"))
	}
	if writeErr != nil {
		return &after.enabled, writeErr
	}
	cached, err = readEnabled(ctx, device, "")
	if err != nil {
		return &after.enabled, err
	}
	if cached != after.enabled {
		return &after.enabled, errors.New("RESTCONF global LLDP configuration did not converge")
	}
	return &after.enabled, device.Persist(ctx)
}
