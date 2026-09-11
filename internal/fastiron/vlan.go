package fastiron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

type VLAN struct {
	ID   int64
	Name string
}

var ErrNotFound = errors.New("FastIron object not found")

const vlanPath = "/network-instances/network-instance=default-vrf/vlans"

type vlanEntry struct {
	ID     int64 `json:"vlan-id"`
	Config struct {
		ID   int64  `json:"vlan-id"`
		Name string `json:"name"`
	} `json:"config"`
}

func ValidateVLAN(v VLAN) error {
	if v.ID < 2 || v.ID > 4094 {
		return errors.New("vlan_id must be between 2 and 4094; the default VLAN is not managed")
	}
	if len(v.Name) > 32 || !regexp.MustCompile(`^[A-Za-z0-9_.:-]*$`).MatchString(v.Name) {
		return errors.New("VLAN name must contain at most 32 letters, digits, underscores, dots, colons, or hyphens")
	}
	return nil
}

func (d *Device) VLAN(ctx context.Context, id int64) (VLAN, error) {
	if id < 1 || id > 4094 {
		return VLAN{}, errors.New("vlan_id must be between 1 and 4094")
	}

	if d.config.Transport == "ssh" {
		return VLAN{}, errors.New("SSH VLAN support awaits verified firmware transcripts")
	}
	if d.rest == nil {
		return VLAN{}, errors.New("RESTCONF is required for VLAN configuration")
	}
	var response struct {
		VLANs []vlanEntry `json:"openconfig-network-instance:vlan"`
	}
	err := d.rest.Do(ctx, http.MethodGet, path.Join(vlanPath, "vlan="+strconv.FormatInt(id, 10)), nil, &response)
	if errors.Is(err, restconf.ErrNotFound) {
		// A missing item URL is also how unsupported endpoints can respond. Only
		// a readable parent collection establishes that this identity is absent.
		var collection struct {
			VLANs *struct {
				VLAN []vlanEntry `json:"vlan"`
			} `json:"openconfig-network-instance:vlans"`
		}
		if err := d.rest.Do(ctx, http.MethodGet, vlanPath, nil, &collection); err != nil {
			return VLAN{}, err
		}
		if collection.VLANs == nil {
			return VLAN{}, errors.New("RESTCONF VLAN collection is missing its configuration container")
		}
		for _, entry := range collection.VLANs.VLAN {
			if entry.ID == id {
				if entry.Config.ID != id {
					return VLAN{}, errors.New("RESTCONF VLAN collection contains an inconsistent identity")
				}
				return VLAN{ID: id, Name: entry.Config.Name}, nil
			}
		}
		return VLAN{}, ErrNotFound
	}
	if err != nil {
		return VLAN{}, err
	}
	if len(response.VLANs) != 1 || response.VLANs[0].ID != id || response.VLANs[0].Config.ID != id {
		return VLAN{}, errors.New("RESTCONF VLAN response is missing the requested identity")
	}
	return VLAN{ID: id, Name: response.VLANs[0].Config.Name}, nil
}

func (d *Device) CheckVLAN(ctx context.Context, v VLAN) error {
	if err := ValidateVLAN(v); err != nil {
		return err
	}
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err := d.VLAN(ctx, v.ID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// ApplyVLAN returns observed state even when persistence fails, allowing the
// resource to retain the remote identity for import-free recovery.
func (d *Device) ApplyVLAN(ctx context.Context, v VLAN) (*VLAN, error) {
	if err := ValidateVLAN(v); err != nil {
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
	current, err := d.VLAN(ctx, v.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	absent := errors.Is(err, ErrNotFound)
	if absent || current != v {
		entry := vlanEntry{ID: v.ID}
		entry.Config.ID = v.ID
		entry.Config.Name = v.Name
		method := http.MethodPatch
		var body any = map[string]any{"vlans": map[string]any{"vlan": []vlanEntry{entry}}}
		if absent {
			method = http.MethodPost
			body = map[string]any{"vlan": []vlanEntry{entry}}
		}
		writeErr := d.rest.Do(ctx, method, vlanPath, body, nil)
		// Read after every attempted write, including ambiguous transport failures.
		observed, readErr := d.VLAN(ctx, v.ID)
		if readErr != nil {
			return nil, errors.Join(writeErr, fmt.Errorf("cannot verify VLAN after write: %w", readErr))
		}
		if observed != v {
			return &observed, errors.Join(writeErr, errors.New("VLAN did not converge to the planned name"))
		}
		current = observed
	}
	// A retry must save even if running state already matches after a failed save.
	if d.config.Persistence == "after_each_write" {
		if err := d.save(ctx); err != nil {
			return &current, err
		}
	}
	return &current, nil
}

func (d *Device) DeleteVLAN(ctx context.Context, id int64) error {
	if err := ValidateVLAN(VLAN{ID: id}); err != nil {
		return err
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err = d.Discover(ctx); err != nil {
		return err
	}
	_, err = d.VLAN(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil {
		// Deleting a parent VLAN must not silently erase separately owned children.
		out, err := d.cli.Run(ctx, true, "show running-config")
		if err != nil {
			return err
		}
		if err := vlanChildren(out[0], id); err != nil {
			return err
		}
		writeErr := d.rest.Do(ctx, http.MethodDelete, path.Join(vlanPath, "vlan="+strconv.FormatInt(id, 10)), nil, nil)
		_, readErr := d.VLAN(ctx, id)
		if !errors.Is(readErr, ErrNotFound) {
			return errors.Join(writeErr, readErr, errors.New("VLAN absence could not be verified"))
		}
	}
	if d.config.Persistence == "after_each_write" {
		return d.save(ctx)
	}
	return nil
}

func vlanChildren(config string, id int64) error {
	if _, err := NormalizeConfiguration(config); err != nil {
		return errors.New("cannot verify VLAN children in running configuration")
	}
	inside := false
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "interface ve "+strconv.FormatInt(id, 10) {
			return errors.New("VLAN has a routed VE interface; remove it before destroying the VLAN")
		}
		if strings.HasPrefix(line, "vlan ") {
			fields := strings.Fields(line)
			inside = len(fields) > 1 && fields[1] == strconv.FormatInt(id, 10)
			continue
		}
		if inside {
			if trimmed == "" || trimmed == "!" {
				continue
			}
			if !strings.HasPrefix(line, " ") {
				inside = false
				continue
			}
			return errors.New("VLAN has child configuration; remove memberships, routed interfaces, and spanning-tree settings before destroying it")
		}
	}
	return nil
}
