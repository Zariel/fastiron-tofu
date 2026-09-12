package igmp

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func applyGlobal(ctx context.Context, device *fastiron.Device, desired settings) (*settings, error) {
	if err := validateGlobal(desired); err != nil {
		return nil, err
	}
	unlock, err := device.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := readGlobal(ctx, device)
	if err != nil {
		return nil, err
	}
	before := current
	endpoint := path.Join(globalPath, "igmp/config")

	// Native configuration is authoritative: even a successful global GET can
	// report the old version or a mode different from the configured one.
	write := func(method, endpoint string, body any) error {
		writeErr := device.DoREST(ctx, method, endpoint, body, nil)
		if method == http.MethodDelete && errors.Is(writeErr, restconf.ErrNotFound) {
			writeErr = nil
		}
		configuration, readErr := device.RunningConfig(ctx)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		observed, readErr := parseGlobal(configuration)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		current = observed
		if !slices.Equal(observed.unowned, before.unowned) {
			return errors.Join(writeErr, errors.New("global IGMP mutation changed unrelated configuration"))
		}
		return writeErr
	}
	patch := func(values settings) error {
		return write(http.MethodPatch, endpoint, map[string]settings{"config": values})
	}

	if current.Mode != desired.Mode {
		leaf := path.Join(endpoint, "querier-mode")
		if err := write(http.MethodDelete, leaf, nil); err != nil {
			return &current.settings, err
		}
		mode := desired.Mode
		if mode == "disabled" {
			mode = ""
		}
		if mode == "" && current.Mode != "disabled" {
			mode = "passive"
		}
		if mode != "" {
			if err := patch(settings{Mode: mode}); err != nil {
				return &current.settings, err
			}
		}
		// PATCH disabled can select passive instead. Reset the explicit mode to
		// disable it, materializing a missing RESTCONF entry first when necessary.
		if desired.Mode == "disabled" && current.Mode != "disabled" {
			if err := write(http.MethodDelete, leaf, nil); err != nil {
				return &current.settings, err
			}
		}
	}
	if current.Version != desired.Version {
		leaf := path.Join(endpoint, "version")
		if err := write(http.MethodDelete, leaf, nil); err != nil {
			return &current.settings, err
		}
		if desired.Version == 3 || current.Version != 2 {
			if err := patch(settings{Version: 3}); err != nil {
				return &current.settings, err
			}
		}
		if desired.Version == 2 && current.Version != 2 {
			if err := write(http.MethodDelete, leaf, nil); err != nil {
				return &current.settings, err
			}
		}
	}
	if current.settings != desired {
		return &current.settings, errors.New("global IGMP configuration did not converge")
	}
	return &current.settings, device.Persist(ctx)
}
