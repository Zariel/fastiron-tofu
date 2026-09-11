package ethernet

import (
	"context"
	"errors"
	"net/http"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type config struct {
	Port, PortName string
	Enabled        bool
}

func validate(v config) error {
	if !interfaceid.EthernetPort(v.Port) {
		return errors.New("port must use stack/slot/port syntax with positive numbers and no leading zeros")
	}
	return interfaceid.ValidatePortName(v.PortName)
}

func Read(ctx context.Context, d *fastiron.Device, port string) (config, error) {
	if err := validate(config{Port: port}); err != nil {
		return config{}, err
	}
	if !d.RESTCONFEnabled() {
		return config{}, errors.New("base Ethernet configuration currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name   string `json:"name"`
				Config *struct {
					Name        string  `json:"name"`
					Description *string `json:"description"`
					Enabled     *bool   `json:"enabled"`
				} `json:"config"`
			} `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.DoREST(ctx, http.MethodGet, "/interfaces", nil, &response); err != nil {
		return config{}, err
	}
	if response.Interfaces == nil {
		return config{}, errors.New("RESTCONF interface response is missing its configuration container")
	}
	if len(response.Interfaces.Interface) == 0 {
		return config{}, errors.New("RESTCONF interface collection is empty; cannot confirm Ethernet state")
	}
	for _, entry := range response.Interfaces.Interface {
		if entry.Name != "ethernet "+port {
			continue
		}
		if entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Description == nil || entry.Config.Enabled == nil {
			return config{}, errors.New("RESTCONF Ethernet response is missing owned configuration fields")
		}
		return config{Port: port, PortName: *entry.Config.Description, Enabled: *entry.Config.Enabled}, nil
	}
	return config{}, fastiron.ErrNotFound
}

func check(ctx context.Context, d *fastiron.Device, v config) error {
	if err := validate(v); err != nil {
		return err
	}
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err := Read(ctx, d, v.Port)
	return err
}

func apply(ctx context.Context, d *fastiron.Device, v config) (*config, error) {
	if err := validate(v); err != nil {
		return nil, err
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err = d.Discover(ctx); err != nil {
		return nil, err
	}
	current, err := Read(ctx, d, v.Port)
	if err != nil {
		return nil, err
	}
	if current != v {
		config := map[string]any{"name": "ethernet " + v.Port, "type": "iana-if-type:ethernetCsmacd"}
		if current.PortName != v.PortName {
			config["description"] = v.PortName
		}
		if current.Enabled != v.Enabled {
			config["enabled"] = v.Enabled
		}
		body := map[string]any{"interfaces": map[string]any{"interface": []any{map[string]any{"name": "ethernet " + v.Port, "config": config}}}}
		writeErr := d.DoREST(ctx, http.MethodPatch, "/interfaces", body, nil)
		observed, readErr := Read(ctx, d, v.Port)
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		if observed != v {
			return &observed, errors.Join(writeErr, errors.New("Ethernet configuration did not converge"))
		}
		current = observed
	}
	return &current, d.Persist(ctx)
}
