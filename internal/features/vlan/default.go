package vlan

import (
	"context"
	"errors"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func defaultID(configuration string) (int64, error) {
	document, err := config.Parse(configuration)
	if err != nil {
		return 0, err
	}
	return document.DefaultVLAN()
}

func rejectDefault(ctx context.Context, device *fastiron.Device, id int64) error {
	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return err
	}
	current, err := defaultID(configuration)
	if err != nil {
		return err
	}
	if id == current {
		return errors.New("the default VLAN is not managed by fastiron_vlan")
	}
	return nil
}
