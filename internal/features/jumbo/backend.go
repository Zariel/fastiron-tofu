package jumbo

import (
	"context"
	"errors"
	"net/http"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type nativeState struct {
	enabled bool
	unowned []string
}

func read(ctx context.Context, device *fastiron.Device) (nativeState, error) {
	if _, err := device.Discover(ctx); err != nil {
		return nativeState{}, err
	}
	var response struct {
		Jumbo *struct {
			Config *struct {
				Enabled *bool `json:"enabled"`
			} `json:"config"`
		} `json:"icx-openconfig-jumbo:jumbo"`
	}
	if err := device.DoREST(ctx, http.MethodGet, "/jumbo", nil, &response); err != nil {
		return nativeState{}, err
	}
	if response.Jumbo == nil || response.Jumbo.Config == nil || response.Jumbo.Config.Enabled == nil {
		return nativeState{}, errors.New("RESTCONF jumbo response omitted its configured value")
	}
	// Both RESTCONF flags can remain stale after native CLI configuration changes.
	output, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	document, err := config.Parse(output)
	if err != nil {
		return nativeState{}, err
	}
	enabled, unowned, err := document.Jumbo()
	return nativeState{enabled: enabled, unowned: unowned}, err
}
