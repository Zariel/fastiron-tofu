package voicevlan

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

type nativeState struct {
	vlanID  int64
	unowned []string
}

func validateInterface(name string) error {
	if !strings.HasPrefix(name, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return errors.New("interface must be a canonical Ethernet name: ethernet <stack>/<slot>/<port>")
	}
	return nil
}

func endpoint(name string) string {
	return path.Join("/interfaces/interface", url.PathEscape(name), "ethernet/config")
}

func read(ctx context.Context, device *fastiron.Device, name string) (nativeState, error) {
	if err := validateInterface(name); err != nil {
		return nativeState{}, err
	}
	if _, err := device.Discover(ctx); err != nil {
		return nativeState{}, err
	}
	var response struct {
		Config *struct{} `json:"openconfig-if-ethernet:config"`
	}
	if err := device.DoREST(ctx, http.MethodGet, endpoint(name), nil, &response); err != nil {
		return nativeState{}, err
	}
	if response.Config == nil {
		return nativeState{}, errors.New("RESTCONF Ethernet response omitted its configuration container")
	}
	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	return parse(configuration, name)
}

func parse(configuration, name string) (nativeState, error) {
	document, err := config.Parse(configuration)
	if err != nil {
		return nativeState{}, err
	}
	observed, err := document.InterfacePolicy(name, config.VoiceVLAN)
	if err != nil {
		return nativeState{}, err
	}
	return nativeState{vlanID: observed.VLAN, unowned: observed.Remaining}, nil
}

// A zero desired ID removes the local policy; it does not restore a prior value.
func apply(ctx context.Context, device *fastiron.Device, name string, desired int64) (*int64, error) {
	if err := validateInterface(name); err != nil {
		return nil, err
	}
	if desired < 0 || desired > 4095 {
		return nil, errors.New("voice VLAN must be between 1 and 4095, or zero for removal")
	}
	unlock, err := device.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := read(ctx, device, name)
	if err != nil {
		return nil, err
	}
	before := current
	write := func(method, target string, body any) error {
		writeErr := device.DoREST(ctx, method, target, body, nil)
		if method == http.MethodDelete && errors.Is(writeErr, restconf.ErrNotFound) {
			writeErr = nil
		}
		configuration, readErr := device.RunningConfig(ctx)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		observed, readErr := parse(configuration, name)
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		current = observed
		if !slices.Equal(current.unowned, before.unowned) {
			return errors.Join(writeErr, errors.New("voice VLAN mutation changed unrelated configuration"))
		}
		return writeErr
	}

	if current.vlanID != desired {
		leaf := path.Join(endpoint(name), "ip-voice-vlan")
		// Removing cached metadata forces PATCH to run the native callback after drift.
		if err := write(http.MethodDelete, leaf, nil); err != nil {
			return &current.vlanID, err
		}
		id := desired
		if id == 0 {
			// Native-only configuration has no deletable RESTCONF entry. Materialize
			// the current value before deleting, verifying native state after each step.
			id = current.vlanID
		}
		if id != 0 {
			body := map[string]any{"openconfig-if-ethernet:config": map[string]any{"ip-voice-vlan": id}}
			if err := write(http.MethodPatch, endpoint(name), body); err != nil {
				return &current.vlanID, err
			}
		}
		if desired == 0 && current.vlanID != 0 {
			if err := write(http.MethodDelete, leaf, nil); err != nil {
				return &current.vlanID, err
			}
		}
	}
	if current.vlanID != desired {
		return &current.vlanID, errors.New("native interface voice VLAN did not converge")
	}
	return &current.vlanID, device.Persist(ctx)
}
