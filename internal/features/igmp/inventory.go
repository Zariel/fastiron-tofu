package igmp

import (
	"context"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func readInventory(ctx context.Context, device *fastiron.Device) (map[int64]settings, error) {
	if _, err := device.Discover(ctx); err != nil {
		return nil, err
	}
	if err := checkRESTCONF(ctx, device); err != nil {
		return nil, err
	}
	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	return parseInventory(configuration)
}

func parseInventory(configuration string) (map[int64]settings, error) {
	document, err := config.Parse(configuration)
	if err != nil {
		return nil, err
	}
	vlans, err := document.VLANs()
	if err != nil {
		return nil, err
	}
	inventory := make(map[int64]settings, len(vlans))
	for id, scope := range vlans {
		observed, err := document.IGMP(scope)
		if err != nil {
			return nil, err
		}
		inventory[id] = settings{Mode: observed.Mode, Version: observed.Version}
	}
	return inventory, nil
}
