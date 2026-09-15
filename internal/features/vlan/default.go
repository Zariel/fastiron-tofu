package vlan

import (
	"context"
	"errors"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func rejectDefault(ctx context.Context, device *fastiron.Device, id int64) error {
	document, err := device.RunningConfig(ctx)
	if err != nil {
		return err
	}
	current, err := document.DefaultVLAN()
	if err != nil {
		return err
	}
	if id == current {
		return errors.New("the default VLAN is not managed by fastiron_vlan")
	}
	return nil
}
