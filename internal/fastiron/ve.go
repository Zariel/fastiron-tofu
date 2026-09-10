package fastiron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type VE struct {
	ID, VLANID int64
	PortName   string
}

func ValidateVE(v VE) error {
	if err := ValidateVLAN(VLAN{ID: v.ID}); err != nil {
		return err
	}
	if v.ID != v.VLANID {
		return errors.New("ve_id and vlan_id must match the FastIron routed VLAN identity")
	}
	if err := validatePortName(v.PortName); err != nil {
		return err
	}
	return nil
}

func (d *Device) VE(ctx context.Context, id int64) (VE, error) {
	if err := ValidateVE(VE{ID: id, VLANID: id}); err != nil {
		return VE{}, err
	}
	if d.config.Transport == "ssh" || d.rest == nil {
		return VE{}, errors.New("VE configuration currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name   string `json:"name"`
				Config *struct {
					Name        string `json:"name"`
					Type        string `json:"type"`
					Description string `json:"description"`
				} `json:"config"`
				Routed *struct {
					Config *struct {
						VLAN int64 `json:"vlan"`
					} `json:"config"`
				} `json:"openconfig-vlan:routed-vlan"`
			} `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.rest.Do(ctx, http.MethodGet, "/interfaces", nil, &response); err != nil {
		return VE{}, err
	}
	if response.Interfaces == nil {
		return VE{}, errors.New("RESTCONF interface collection is missing its container")
	}
	if len(response.Interfaces.Interface) == 0 {
		return VE{}, errors.New("RESTCONF interface collection is empty; cannot confirm VE state")
	}
	name := "ve " + strconv.FormatInt(id, 10)
	for _, entry := range response.Interfaces.Interface {
		if entry.Name != name {
			continue
		}
		if entry.Config == nil || entry.Config.Name != name || entry.Config.Type != "iana-if-type:l3ipvlan" || entry.Routed == nil || entry.Routed.Config == nil {
			return VE{}, errors.New("RESTCONF VE response is missing its identity or VLAN binding")
		}
		return VE{ID: id, VLANID: entry.Routed.Config.VLAN, PortName: entry.Config.Description}, nil
	}
	return VE{}, ErrNotFound
}

func (d *Device) CheckVE(ctx context.Context, v VE) error {
	if err := ValidateVE(v); err != nil {
		return err
	}
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err := d.VE(ctx, v.ID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (d *Device) ApplyVE(ctx context.Context, v VE) (*VE, error) {
	if err := ValidateVE(v); err != nil {
		return nil, err
	}
	unlock, err := d.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	if _, err := d.VLAN(ctx, v.VLANID); err != nil {
		return nil, fmt.Errorf("VE requires an existing VLAN: %w", err)
	}
	current, err := d.VE(ctx, v.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	absent := errors.Is(err, ErrNotFound)
	if !absent && current.VLANID != v.VLANID {
		return &current, errors.New("existing VE belongs to a different VLAN")
	}
	if absent || current != v {
		name := "ve " + strconv.FormatInt(v.ID, 10)
		entry := map[string]any{"name": name, "config": map[string]any{"name": name, "type": "iana-if-type:l3ipvlan", "description": v.PortName}}
		method := http.MethodPatch
		body := map[string]any{"interfaces": map[string]any{"interface": []any{entry}}}
		if absent {
			method = http.MethodPost
			entry["openconfig-vlan:routed-vlan"] = map[string]any{"config": map[string]any{"vlan": v.VLANID}}
			body = map[string]any{"interface": []any{entry}}
		}
		writeErr := d.rest.Do(ctx, method, "/interfaces", body, nil)
		observed, readErr := d.VE(ctx, v.ID)
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		if observed != v {
			return &observed, errors.Join(writeErr, errors.New("VE configuration did not converge"))
		}
		current = observed
	}
	if d.config.Persistence == "after_each_write" {
		return &current, d.save(ctx)
	}
	return &current, nil
}

func (d *Device) DeleteVE(ctx context.Context, id int64) error {
	if err := ValidateVE(VE{ID: id, VLANID: id}); err != nil {
		return err
	}
	unlock, err := d.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err = d.VE(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil {
		// Removing a logical interface can erase addresses, protocol bindings, and
		// other independent configuration. Verify children before deleting the parent.
		output, err := d.cli.Run(ctx, true, "show running-config")
		if err != nil {
			return err
		}
		name := "ve " + strconv.FormatInt(id, 10)
		if err := veChildren(output[0], name); err != nil {
			return err
		}
		writeErr := d.rest.Do(ctx, http.MethodDelete, "/interfaces/interface="+url.PathEscape(name), nil, nil)
		_, readErr := d.VE(ctx, id)
		if !errors.Is(readErr, ErrNotFound) {
			return errors.Join(writeErr, readErr, errors.New("VE absence could not be verified"))
		}
	}
	if d.config.Persistence == "after_each_write" {
		return d.save(ctx)
	}
	return nil
}

func veChildren(config, name string) error {
	if _, err := configuration(config); err != nil {
		return err
	}
	inside := false
	found := false
	for _, line := range strings.Split(config, "\n") {
		if line == "interface "+name {
			inside = true
			found = true
			continue
		}
		if !inside {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "!" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			break
		}
		if strings.HasPrefix(trimmed, "port-name ") {
			continue
		}
		return errors.New("VE has child configuration; remove addresses, routing bindings, and other settings before destroying it")
	}
	if !found {
		return errors.New("cannot confirm the VE configuration block before deletion")
	}
	return nil
}
