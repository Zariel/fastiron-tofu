package vlan

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func defaultID(configuration string) (int64, error) {
	configuration, err := fastiron.NormalizeConfiguration(configuration)
	if err != nil {
		return 0, err
	}
	id := int64(1)
	found := false
	for _, line := range strings.Split(configuration, "\n") {
		if !strings.HasPrefix(line, "default-vlan-id") {
			continue
		}
		fields := strings.Fields(line)
		if found || len(fields) != 2 || fields[0] != "default-vlan-id" {
			return 0, errors.New("invalid or duplicate native default VLAN setting")
		}
		id, err = strconv.ParseInt(fields[1], 10, 64)
		if err != nil || id < 1 || id > 4095 {
			return 0, errors.New("invalid native default VLAN ID")
		}
		found = true
	}
	return id, nil
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
