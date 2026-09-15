package poe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type port struct {
	Name    string
	unowned []string
	config.PoEPolicy
	PowerClass               *int64
	PowerUsedMilliwatts      *float64
	PowerAllocatedMilliwatts *float64
}

type entry struct {
	Config *struct {
		Enabled *bool `json:"enabled"`
	} `json:"config"`
	State struct {
		PowerClass     *int64       `json:"power-class"`
		PowerUsed      *json.Number `json:"power-used"`
		PowerAllocated *json.Number `json:"power-allocated"`
	} `json:"state"`
}

func validateInterface(name string) error {
	if !strings.HasPrefix(name, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return errors.New("interface must be a canonical Ethernet name: ethernet <stack>/<slot>/<port>")
	}
	return nil
}

func (e entry) telemetry(name string) (port, error) {
	if e.Config == nil {
		return port{}, errors.New("RESTCONF PoE response is missing its configuration container")
	}
	p := port{Name: name, PowerClass: e.State.PowerClass}
	if e.State.PowerUsed != nil {
		power, err := e.State.PowerUsed.Float64()
		if err != nil || power < 0 {
			return port{}, errors.New("RESTCONF PoE power measurement is invalid")
		}
		p.PowerUsedMilliwatts = &power
	}
	if e.State.PowerAllocated != nil {
		power, err := e.State.PowerAllocated.Float64()
		if err != nil || power < 0 {
			return port{}, errors.New("RESTCONF PoE allocated power measurement is invalid")
		}
		p.PowerAllocatedMilliwatts = &power
	}

	return p, nil
}

func readPort(ctx context.Context, d *fastiron.Device, name string) (port, error) {
	if err := validateInterface(name); err != nil {
		return port{}, err
	}
	if !d.RESTCONFEnabled() {
		return port{}, errors.New("PoE configuration currently requires RESTCONF")
	}
	var response struct {
		PoE *entry `json:"icx-openconfig-if-poe-aug:poe"`
	}
	if err := d.ReadREST(ctx, path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/poe"), &response); err != nil {
		return port{}, err
	}
	if response.PoE == nil {
		return port{}, errors.New("RESTCONF interface does not expose PoE")
	}
	observed, err := response.PoE.telemetry(name)
	if err != nil {
		return port{}, err
	}
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return port{}, err
	}
	policy, remaining, err := document.PoE(name)
	if err != nil {
		return port{}, err
	}
	observed.PoEPolicy = policy
	observed.unowned = remaining
	return observed, nil
}

func readPorts(ctx context.Context, d *fastiron.Device) ([]port, error) {
	if !d.RESTCONFEnabled() {
		return nil, errors.New("PoE discovery currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name     string `json:"name"`
				Ethernet struct {
					PoE *entry `json:"icx-openconfig-if-poe-aug:poe"`
				} `json:"openconfig-if-ethernet:ethernet"`
			} `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.ReadREST(ctx, "/interfaces", &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil {
		return nil, errors.New("RESTCONF interface response is missing its collection")
	}
	if len(response.Interfaces.Interface) == 0 {
		return nil, errors.New("RESTCONF interface collection is empty; cannot confirm PoE state")
	}
	ports := []port{}
	for _, entry := range response.Interfaces.Interface {
		if entry.Ethernet.PoE == nil {
			continue
		}
		if err := validateInterface(entry.Name); err != nil {
			return nil, err
		}
		p, err := entry.Ethernet.PoE.telemetry(entry.Name)
		if err != nil {
			return nil, err
		}
		ports = append(ports, p)
	}
	if len(ports) == 0 {
		return ports, nil
	}
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ports {
		policy, _, err := document.PoE(ports[i].Name)
		if err != nil {
			return nil, err
		}
		ports[i].PoEPolicy = policy
	}
	return ports, nil
}

func validatePolicy(policy config.PoEPolicy) error {
	switch {
	case policy.Priority < 1 || policy.Priority > 3:
		return errors.New("PoE priority must be between 1 and 3")
	case policy.PowerByClass < 0 || policy.PowerByClass > 4:
		return errors.New("PoE allocation class must be between 0 and 4")
	case policy.PowerLimitMilliwatts != 0 && (policy.PowerLimitMilliwatts < 1000 || policy.PowerLimitMilliwatts > 95000):
		return errors.New("PoE power limit must be zero or between 1000 and 95000 milliwatts; supported limits depend on port capabilities")
	case policy.PowerByClass != 0 && policy.PowerLimitMilliwatts != 0:
		return errors.New("PoE allocation class and an explicit power limit cannot both be configured")
	case !policy.Enabled && (policy.Priority != 3 || policy.PowerByClass != 0 || policy.PowerLimitMilliwatts != 0):
		return errors.New("disabled PoE requires default priority and allocation because FastIron clears these settings when disabling power")
	default:
		return nil
	}
}

func applyPort(ctx context.Context, d *fastiron.Device, name string, desired config.PoEPolicy) (*port, error) {
	if err := validatePolicy(desired); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*port, error) {
		current, err := readPort(ctx, d, name)
		if err != nil {
			return nil, err
		}
		if current.PoEPolicy == desired {
			return &current, nil
		}

		values := map[string]any{"enabled": desired.Enabled, "priority": desired.Priority}
		if desired.PowerLimitMilliwatts != 0 {
			values["power-limit"] = desired.PowerLimitMilliwatts
		} else {
			values["power-by-class"] = desired.PowerByClass
		}
		// PUT reapplies every policy field even when RESTCONF caches the desired
		// value. PATCH can skip those fields and clear native allocation settings.
		endpoint := path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/poe/config")
		writeErr := update.REST(http.MethodPut, endpoint, map[string]any{"config": values})
		observed, readErr := readPort(ctx, d, name)
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		// Confirm independently owned commands survived before allowing a save.
		if !slices.Equal(current.unowned, observed.unowned) {
			return &observed, errors.Join(writeErr, errors.New("PoE mutation changed unrelated configuration"))
		}
		if observed.PoEPolicy != desired {
			return &observed, errors.Join(writeErr, errors.New("PoE configuration did not converge"))
		}
		return &observed, writeErr
	})
}
