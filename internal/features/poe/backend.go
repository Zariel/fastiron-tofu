package poe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type port struct {
	Name string
	config.PoEPolicy
	PowerClass          *int64
	PowerUsedMilliwatts *float64
}

type entry struct {
	Config *struct {
		Enabled *bool `json:"enabled"`
	} `json:"config"`
	State struct {
		PowerClass *int64       `json:"power-class"`
		PowerUsed  *json.Number `json:"power-used"`
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
	document, err := readNative(ctx, d)
	if err != nil {
		return port{}, err
	}
	policy, _, err := document.PoE(name)
	if err != nil {
		return port{}, err
	}
	observed.PoEPolicy = policy
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
	document, err := readNative(ctx, d)
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

func readNative(ctx context.Context, device *fastiron.Device) (*config.Document, error) {
	output, err := device.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	return config.Parse(output)
}

func applyPort(ctx context.Context, d *fastiron.Device, name string, enabled bool) (*port, error) {
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*port, error) {
		current, err := readPort(ctx, d, name)
		if err != nil {
			return nil, err
		}
		if current.Enabled != enabled {
			// Native enable changes reset allocation and priority. Those settings
			// are outside this resource's current ownership and must not be cleared.
			if current.Priority != 3 || current.PowerByClass != 0 || current.PowerLimitMilliwatts != 0 {
				return &current, errors.New("PoE enable changes require default priority and power allocation; explicit settings are not owned by this resource")
			}
			body := map[string]any{"poe": map[string]any{"config": map[string]any{"enabled": enabled}}}
			writeErr := update.REST(http.MethodPatch, path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/poe"), body)
			observed, readErr := readPort(ctx, d, name)
			if readErr != nil {
				return nil, errors.Join(writeErr, readErr)
			}
			if observed.Enabled != enabled {
				return &observed, errors.Join(writeErr, errors.New("PoE configuration did not converge"))
			}
			if writeErr != nil {
				return &observed, writeErr
			}
			current = observed
		}
		return &current, nil
	})
}
