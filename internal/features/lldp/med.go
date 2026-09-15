package lldp

import (
	"context"
	"maps"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func readMED(ctx context.Context, device *fastiron.Device) (map[string]map[string]config.MEDPolicy, []string, error) {
	if _, err := readMEDCache(ctx, device); err != nil {
		return nil, nil, err
	}
	return readMEDNative(ctx, device)
}

func readMEDNative(ctx context.Context, device *fastiron.Device) (map[string]map[string]config.MEDPolicy, []string, error) {
	inventory, err := readRESTInterfaces(ctx, device)
	if err != nil {
		return nil, nil, err
	}
	document, err := device.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}

	return document.MEDPolicies(slices.Sorted(maps.Keys(inventory)))
}
