package igmp

import (
	"context"
	"errors"
	"strconv"
	"strings"

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
	configuration, err := fastiron.NormalizeConfiguration(configuration)
	if err != nil {
		return nil, err
	}
	vlans := map[int64]settings{}
	var id int64
	for _, line := range strings.Split(configuration, "\n") {
		if line[0] != ' ' && line[0] != '\t' {
			id = 0
			fields := strings.Fields(line)
			if fields[0] != "vlan" {
				continue
			}
			if len(fields) < 2 {
				return nil, errors.New("native VLAN identity is missing")
			}
			id, err = strconv.ParseInt(fields[1], 10, 64)
			if err != nil || id < 1 || id > 4095 {
				return nil, errors.New("native VLAN identity is invalid")
			}
			if _, exists := vlans[id]; exists {
				return nil, errors.New("native configuration repeats a VLAN")
			}
			vlans[id] = settings{}
			continue
		}
		if id == 0 {
			continue
		}
		current := vlans[id]
		if _, err := parseOverride(line, &current); err != nil {
			return nil, err
		}
		vlans[id] = current
	}
	return vlans, nil
}

func parseOverride(line string, current *settings) (bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "multicast" {
		return false, nil
	}
	switch fields[1] {
	case "active", "passive", "disable-igmp-snoop":
		if len(fields) != 2 || current.Mode != "" {
			return false, errors.New("native IGMP mode is ambiguous or unsupported")
		}
		current.Mode = fields[1]
		if current.Mode == "disable-igmp-snoop" {
			current.Mode = "disabled"
		}
	case "version":
		if len(fields) != 3 || current.Version != 0 {
			return false, errors.New("native IGMP version is ambiguous or unsupported")
		}
		version, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || (version != 2 && version != 3) {
			return false, errors.New("native IGMP version is unsupported")
		}
		current.Version = version
	default:
		return false, nil
	}
	return true, nil
}
