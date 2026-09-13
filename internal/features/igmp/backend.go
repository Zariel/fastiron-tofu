package igmp

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
	"strconv"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

const vlanPath = "/igmp-mld-snooping/vlans"

type settings struct {
	Mode    string `json:"querier-mode,omitempty"`
	Version int64  `json:"version,omitempty"`
}

type vlanEntry struct {
	ID    int64 `json:"vlan-id"`
	Proto struct {
		ID   int64 `json:"vlan-id"`
		IGMP struct {
			Config *settings `json:"config"`
		} `json:"igmp"`
	} `json:"proto"`
}

type nativeState struct {
	settings
	unowned []string
}

func validate(id int64, desired settings) error {
	if id < 1 || id > 4095 {
		return errors.New("vlan_id must be between 1 and 4095")
	}
	if desired.Mode != "" && desired.Mode != "active" && desired.Mode != "passive" {
		return errors.New("querier_mode must be active or passive; omit it to remove the VLAN override")
	}
	if desired.Version != 0 && desired.Version != 2 && desired.Version != 3 {
		return errors.New("version must be 2 or 3; omit it to remove the VLAN override")
	}
	return nil
}

func checkRESTCONF(ctx context.Context, device *fastiron.Device) error {
	var response struct {
		VLANs *struct {
			VLAN []vlanEntry `json:"vlan"`
		} `json:"icx-igmp-mld-snooping:vlans"`
	}
	if err := device.DoREST(ctx, http.MethodGet, vlanPath, nil, &response); err != nil {
		return err
	}
	if response.VLANs == nil {
		return errors.New("RESTCONF IGMP response omitted the VLAN container")
	}
	seen := map[int64]bool{}
	for _, entry := range response.VLANs.VLAN {
		if entry.ID < 1 || entry.ID > 4095 || entry.Proto.ID != entry.ID || seen[entry.ID] {
			return errors.New("RESTCONF IGMP response contains an invalid or duplicate VLAN identity")
		}
		seen[entry.ID] = true
	}
	return nil
}

func read(ctx context.Context, device *fastiron.Device, id int64) (nativeState, error) {
	if err := validate(id, settings{}); err != nil {
		return nativeState{}, err
	}
	if _, err := device.Discover(ctx); err != nil {
		return nativeState{}, err
	}
	if err := checkRESTCONF(ctx, device); err != nil {
		return nativeState{}, err
	}
	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	return parse(configuration, id)
}

func parse(configuration string, id int64) (nativeState, error) {
	document, err := config.Parse(configuration)
	if err != nil {
		return nativeState{}, err
	}
	vlans, err := document.VLANs()
	if err != nil {
		return nativeState{}, err
	}
	scope, found := vlans[id]
	if !found {
		return nativeState{}, fastiron.ErrNotFound
	}
	observed, err := document.IGMP(scope)
	if err != nil {
		return nativeState{}, err
	}
	return nativeState{settings: settings{Mode: observed.Mode, Version: observed.Version}, unowned: observed.Remaining}, nil
}

func apply(ctx context.Context, device *fastiron.Device, id int64, desired settings, present bool) (*settings, error) {
	if !present {
		desired = settings{}
	}
	if err := validate(id, desired); err != nil {
		return nil, err
	}
	unlock, err := device.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := read(ctx, device, id)
	if errors.Is(err, fastiron.ErrNotFound) && !present {
		return nil, device.Persist(ctx)
	}
	if err != nil {
		return nil, err
	}
	before := current
	endpoint := path.Join(vlanPath, "vlan", strconv.FormatInt(id, 10), "proto/igmp/config")

	// Each mutation verifies native overrides and preserves every unowned command.
	// RESTCONF can omit a native disable flag, so its echo is not a convergence check.
	write := func(method, endpoint string, body any) error {
		writeErr := device.DoREST(ctx, method, endpoint, body, nil)
		if method == http.MethodDelete && errors.Is(writeErr, restconf.ErrNotFound) {
			// Missing metadata does not prove the native override is absent.
			writeErr = nil
		}
		configuration, readErr := device.RunningConfig(ctx)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		observed, readErr := parse(configuration, id)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		current = observed
		if !slices.Equal(observed.unowned, before.unowned) {
			return errors.Join(writeErr, errors.New("IGMP mutation changed unrelated configuration"))
		}
		return writeErr
	}
	patch := func(values settings) error {
		entry := vlanEntry{ID: id}
		entry.Proto.ID = id
		entry.Proto.IGMP.Config = &values
		body := map[string]any{"icx-igmp-mld-snooping:vlans": map[string]any{"vlan": []vlanEntry{entry}}}
		return write(http.MethodPatch, vlanPath, body)
	}

	// Replacing a changed leaf forces its native callback even if RESTCONF
	// already caches the desired value. Unchanged leaves retain their ownership.
	if current.Mode != desired.Mode {
		leaf := path.Join(endpoint, "querier-mode")
		if err := write(http.MethodDelete, leaf, nil); err != nil {
			return &current.settings, err
		}
		mode := desired.Mode
		if mode == "" && current.Mode != "" {
			// A native-only override needs a deletable RESTCONF entry. Passive
			// mode also clears the native disable flag without sending queries.
			mode = "passive"
		}
		if mode != "" {
			if err := patch(settings{Mode: mode}); err != nil {
				return &current.settings, err
			}
		}
		if desired.Mode == "" && current.Mode != "" {
			if err := write(http.MethodDelete, leaf, nil); err != nil {
				return &current.settings, err
			}
		}
	}
	if current.Version != desired.Version {
		leaf := path.Join(endpoint, "version")
		if err := write(http.MethodDelete, leaf, nil); err != nil {
			return &current.settings, err
		}
		version := desired.Version
		if version == 0 {
			version = current.Version
		}
		if version != 0 {
			if err := patch(settings{Version: version}); err != nil {
				return &current.settings, err
			}
		}
		if desired.Version == 0 && current.Version != 0 {
			if err := write(http.MethodDelete, leaf, nil); err != nil {
				return &current.settings, err
			}
		}
	}
	if current.settings != desired {
		return &current.settings, errors.New("native IGMP overrides did not converge")
	}
	return &current.settings, device.Persist(ctx)
}
