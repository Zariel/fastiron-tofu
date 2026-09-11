package fastiron

import (
	"context"
	"errors"
	"net/http"
	"regexp"
)

type Ethernet struct {
	Port, PortName string
	Enabled        bool
}

var portPattern = regexp.MustCompile(`^[1-9][0-9]*/[1-9][0-9]*/[1-9][0-9]*$`)

func ValidateEthernet(v Ethernet) error {
	if !portPattern.MatchString(v.Port) {
		return errors.New("port must use stack/slot/port syntax with positive numbers and no leading zeros")
	}
	return validatePortName(v.PortName)
}

func validatePortName(name string) error {
	if len(name) > 64 {
		return errors.New("port_name must contain at most 64 bytes")
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return errors.New("port_name cannot contain control characters")
		}
	}
	return nil
}

func (d *Device) Ethernet(ctx context.Context, port string) (Ethernet, error) {
	if err := ValidateEthernet(Ethernet{Port: port}); err != nil {
		return Ethernet{}, err
	}
	if d.config.Transport == "ssh" || d.rest == nil {
		return Ethernet{}, errors.New("base Ethernet configuration currently requires RESTCONF")
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
	if err := d.rest.Do(ctx, http.MethodGet, "/interfaces", nil, &response); err != nil {
		return Ethernet{}, err
	}
	if response.Interfaces == nil {
		return Ethernet{}, errors.New("RESTCONF interface response is missing its configuration container")
	}
	if len(response.Interfaces.Interface) == 0 {
		return Ethernet{}, errors.New("RESTCONF interface collection is empty; cannot confirm Ethernet state")
	}
	for _, entry := range response.Interfaces.Interface {
		if entry.Name != "ethernet "+port {
			continue
		}
		if entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Description == nil || entry.Config.Enabled == nil {
			return Ethernet{}, errors.New("RESTCONF Ethernet response is missing owned configuration fields")
		}
		return Ethernet{Port: port, PortName: *entry.Config.Description, Enabled: *entry.Config.Enabled}, nil
	}
	return Ethernet{}, ErrNotFound
}

func (d *Device) CheckEthernet(ctx context.Context, v Ethernet) error {
	if err := ValidateEthernet(v); err != nil {
		return err
	}
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err := d.Ethernet(ctx, v.Port)
	return err
}

func (d *Device) ApplyEthernet(ctx context.Context, v Ethernet) (*Ethernet, error) {
	if err := ValidateEthernet(v); err != nil {
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
	current, err := d.Ethernet(ctx, v.Port)
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
		writeErr := d.rest.Do(ctx, http.MethodPatch, "/interfaces", body, nil)
		observed, readErr := d.Ethernet(ctx, v.Port)
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		if observed != v {
			return &observed, errors.Join(writeErr, errors.New("Ethernet configuration did not converge"))
		}
		current = observed
	}
	if d.config.Persistence == "after_each_write" {
		if err := d.save(ctx); err != nil {
			return &current, err
		}
	}
	return &current, nil
}
