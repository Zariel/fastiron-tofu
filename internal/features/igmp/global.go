package igmp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
	if err := device.DoREST(ctx, http.MethodGet, globalPath, nil, &response); err != nil {
		return nativeState{}, err
	}
	if response.Global == nil || response.Global.IGMP == nil {
		return nativeState{}, errors.New("RESTCONF omitted the global IGMP container")
	}
	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	return parseGlobal(configuration)
}

func parseGlobal(configuration string) (nativeState, error) {
	configuration, err := fastiron.NormalizeConfiguration(configuration)
	if err != nil {
		return nativeState{}, err
	}
	state := nativeState{settings: settings{Mode: "disabled", Version: 2}}
	modeSeen, versionSeen := false, false
	for _, line := range strings.Split(configuration, "\n") {
		if line == "ip multicast" {
			return nativeState{}, errors.New("global IGMP mode is missing")
		}
		if !strings.HasPrefix(line, "ip multicast ") {
			state.unowned = append(state.unowned, line)
			continue
		}
		fields := strings.Fields(line)
		switch fields[2] {
		case "active", "passive":
			if len(fields) != 3 || modeSeen {
				return nativeState{}, errors.New("global IGMP mode is ambiguous or unsupported")
			}
			state.Mode = fields[2]
			modeSeen = true
		case "version":
			if len(fields) != 4 || versionSeen {
				return nativeState{}, errors.New("global IGMP version is ambiguous or unsupported")
			}
			version, err := strconv.ParseInt(fields[3], 10, 64)
			if err != nil || (version != 2 && version != 3) {
				return nativeState{}, errors.New("global IGMP version is unsupported")
			}
			state.Version = version
			versionSeen = true
		default:
			state.unowned = append(state.unowned, line)
		}
	}
	return state, nil
}
