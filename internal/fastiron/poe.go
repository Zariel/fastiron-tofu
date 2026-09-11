package fastiron

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type PoEInterface struct {
	Name                string
	Enabled             bool
	PowerClass          *int64
	PowerUsedMilliwatts *float64
}

type poeEntry struct {
	Config *struct {
		Enabled *bool `json:"enabled"`
	} `json:"config"`
	State struct {
		PowerClass *int64       `json:"power-class"`
		PowerUsed  *json.Number `json:"power-used"`
	} `json:"state"`
}

func ValidatePoEInterface(name string) error {
	if !strings.HasPrefix(name, "ethernet ") || !portPattern.MatchString(strings.TrimPrefix(name, "ethernet ")) {
		return errors.New("interface must be a canonical Ethernet name: ethernet <stack>/<slot>/<port>")
	}
	return nil
}

func (e poeEntry) observed(name string) (PoEInterface, error) {
	if e.Config == nil {
		return PoEInterface{}, errors.New("RESTCONF PoE response is missing its configuration container")
	}
	// Native enabled-leaf deletion restores inline power while omitting the leaf.
	// Operational enabled may lag that reset and is not configuration evidence.
	p := PoEInterface{Name: name, Enabled: e.Config.Enabled == nil || *e.Config.Enabled, PowerClass: e.State.PowerClass}
	if e.State.PowerUsed != nil {
		power, err := e.State.PowerUsed.Float64()
		if err != nil || power < 0 {
			return PoEInterface{}, errors.New("RESTCONF PoE power measurement is invalid")
		}
		p.PowerUsedMilliwatts = &power
	}
	return p, nil
}

func (d *Device) PoE(ctx context.Context, name string) (PoEInterface, error) {
	if err := ValidatePoEInterface(name); err != nil {
		return PoEInterface{}, err
	}
	if d.config.Transport == "ssh" || d.rest == nil {
		return PoEInterface{}, errors.New("PoE configuration currently requires RESTCONF")
	}
	var response struct {
		PoE *poeEntry `json:"icx-openconfig-if-poe-aug:poe"`
	}
	if err := d.rest.Do(ctx, http.MethodGet, path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/poe"), nil, &response); err != nil {
		return PoEInterface{}, err
	}
	if response.PoE == nil {
		return PoEInterface{}, errors.New("RESTCONF interface does not expose PoE")
	}
	return response.PoE.observed(name)
}

func (d *Device) PoEInterfaces(ctx context.Context) ([]PoEInterface, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("PoE discovery currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name     string `json:"name"`
				Ethernet struct {
					PoE *poeEntry `json:"icx-openconfig-if-poe-aug:poe"`
				} `json:"openconfig-if-ethernet:ethernet"`
			} `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.rest.Do(ctx, http.MethodGet, "/interfaces", nil, &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil {
		return nil, errors.New("RESTCONF interface response is missing its collection")
	}
	if len(response.Interfaces.Interface) == 0 {
		return nil, errors.New("RESTCONF interface collection is empty; cannot confirm PoE state")
	}
	ports := []PoEInterface{}
	for _, entry := range response.Interfaces.Interface {
		if entry.Ethernet.PoE == nil {
			continue
		}
		if err := ValidatePoEInterface(entry.Name); err != nil {
			return nil, err
		}
		p, err := entry.Ethernet.PoE.observed(entry.Name)
		if err != nil {
			return nil, err
		}
		ports = append(ports, p)
	}
	return ports, nil
}

func (d *Device) ApplyPoE(ctx context.Context, name string, enabled bool) (*PoEInterface, error) {
	unlock, err := d.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	current, err := d.PoE(ctx, name)
	if err != nil {
		return nil, err
	}
	if current.Enabled != enabled {
		body := map[string]any{"poe": map[string]any{"config": map[string]any{"enabled": enabled}}}
		writeErr := d.rest.Do(ctx, http.MethodPatch, path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/poe"), body, nil)
		observed, readErr := d.PoE(ctx, name)
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		if observed.Enabled != enabled {
			return &observed, errors.Join(writeErr, errors.New("PoE configuration did not converge"))
		}
		current = observed
	}
	if d.config.Persistence == "after_each_write" {
		return &current, d.save(ctx)
	}
	return &current, nil
}
