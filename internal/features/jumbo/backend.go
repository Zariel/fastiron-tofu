package jumbo

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type observation struct {
	active  bool
	enabled bool
	cached  bool
	unowned []string
}

func read(ctx context.Context, device *fastiron.Device) (observation, error) {
	if _, err := device.Discover(ctx); err != nil {
		return observation{}, err
	}
	var response struct {
		Jumbo *struct {
			Config *struct {
				Enabled *bool `json:"enabled"`
			} `json:"config"`
			Active *struct {
				Enabled *bool `json:"enabled"`
			} `json:"operation-state"`
		} `json:"icx-openconfig-jumbo:jumbo"`
	}
	if err := device.DoREST(ctx, http.MethodGet, "/jumbo", nil, &response); err != nil {
		return observation{}, err
	}
	if response.Jumbo == nil || response.Jumbo.Config == nil || response.Jumbo.Config.Enabled == nil {
		return observation{}, errors.New("RESTCONF jumbo response omitted its configured value")
	}
	if response.Jumbo.Active == nil || response.Jumbo.Active.Enabled == nil {
		return observation{}, errors.New("RESTCONF jumbo response omitted its active value")
	}
	observed, err := readNative(ctx, device)
	observed.cached = *response.Jumbo.Config.Enabled
	observed.active = *response.Jumbo.Active.Enabled
	return observed, err
}

func readNative(ctx context.Context, device *fastiron.Device) (observation, error) {
	// The configured RESTCONF flag can remain stale after native CLI changes.
	output, err := device.RunningConfig(ctx)
	if err != nil {
		return observation{}, err
	}
	document, err := config.Parse(output)
	if err != nil {
		return observation{}, err
	}
	enabled, unowned, err := document.Jumbo()
	return observation{enabled: enabled, unowned: unowned}, err
}

func apply(ctx context.Context, device *fastiron.Device, enabled bool) (*observation, error) {
	unlock, err := device.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := read(ctx, device)
	if err != nil {
		return nil, err
	}
	if current.enabled == enabled {
		return &current, device.Persist(ctx)
	}

	before := current
	ctx, cancel := context.WithTimeout(ctx, device.RESTCONFTimeout())
	defer cancel()
	// A cached desired value can suppress the native callback. Wait for the
	// firmware to synchronize it; rewriting the current native value can fail.
	for current.cached != current.enabled && current.enabled != enabled {
		select {
		case <-ctx.Done():
			return &current, errors.Join(errors.New("RESTCONF jumbo configuration did not synchronize with native state"), ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
		next, err := read(ctx, device)
		if err != nil {
			return &current, err
		}
		current = next
		if !slices.Equal(before.unowned, current.unowned) {
			return &current, errors.New("unrelated configuration changed while waiting for jumbo synchronization")
		}
	}
	if current.enabled == enabled {
		return &current, device.Persist(ctx)
	}

	body := map[string]any{"icx-openconfig-jumbo:jumbo": map[string]any{"config": map[string]bool{"enabled": enabled}}}
	writeErr := device.DoREST(ctx, http.MethodPut, "/jumbo", body, nil)
	for {
		next, readErr := readNative(ctx, device)
		if readErr != nil {
			return &current, errors.Join(writeErr, readErr)
		}
		// Active mode changes on reload, not through this configuration write.
		next.active = current.active
		current = next
		// Verify native effects and every unowned command before permitting a save.
		if !slices.Equal(before.unowned, current.unowned) {
			return &current, errors.Join(writeErr, errors.New("jumbo mutation changed unrelated configuration"))
		}
		if writeErr != nil {
			return &current, writeErr
		}
		if current.enabled == enabled {
			return &current, device.Persist(ctx)
		}
		select {
		case <-ctx.Done():
			return &current, errors.Join(errors.New("native jumbo configuration did not converge"), ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
