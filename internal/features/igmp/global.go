package igmp

import (
	"context"
	"errors"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

const globalPath = "/igmp-mld-snooping/global"

func validateGlobal(value settings) error {
	if value.Mode != "active" && value.Mode != "passive" && value.Mode != "disabled" {
		return errors.New("querier_mode must be active, passive or disabled")
	}
	if value.Version != 2 && value.Version != 3 {
		return errors.New("version must be 2 or 3")
	}
	return nil
}

func readGlobal(ctx context.Context, device *fastiron.Device) (nativeState, error) {
	if _, err := device.Discover(ctx); err != nil {
		return nativeState{}, err
	}
	var response struct {
		Global *struct {
			IGMP *struct {
				Config *settings `json:"config"`
			} `json:"igmp"`
		} `json:"icx-igmp-mld-snooping:global"`
	}
	if err := device.ReadREST(ctx, globalPath, &response); err != nil {
		return nativeState{}, err
	}
	if response.Global == nil || response.Global.IGMP == nil {
		return nativeState{}, errors.New("RESTCONF omitted the global IGMP container")
	}
	document, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	return parseGlobal(document)
}

func parseGlobal(document *config.Document) (nativeState, error) {
	observed, err := document.IGMP(-1)
	if err != nil {
		return nativeState{}, err
	}
	return nativeState{settings: settings{Mode: observed.Mode, Version: observed.Version}, unowned: observed.Remaining}, nil
}
