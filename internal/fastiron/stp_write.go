package fastiron

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
)

func (d *Device) ApplySTPVLAN(ctx context.Context, v STPVLAN, present bool) (*STPVLAN, error) {
	if err := ValidateSTPVLAN(v); err != nil {
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
	vlans, err := d.STPVLANs(ctx)
	if err != nil {
		return nil, err
	}
	var current *STPVLAN
	for _, entry := range vlans {
		if entry.VLANID == v.VLANID {
			current = &entry
		}
	}
	if present {
		if _, err := d.VLAN(ctx, v.VLANID); err != nil {
			return nil, err
		}
		if current != nil && current.Mode != v.Mode {
			return nil, errors.New("VLAN uses another spanning-tree mode; import its configuration before replacing the mode")
		}
	}
	if current != nil && !present {
		output, err := d.cli.Run(ctx, true, "show running-config")
		if err != nil {
			return current, err
		}
		if err := stpVLANChildren(output[0], *current); err != nil {
			return current, err
		}
	}
	// Removing RSTP can leave classic STP enabled. Derive that remaining state
	// and remove it once; never repeat a deletion against an unchanged mode.
	removed := map[string]bool{}
	for (present && (current == nil || *current != v)) || (!present && current != nil) {
		method := http.MethodPost
		endpoint := stpVLANPath(v.Mode)
		var body any
		if present {
			priority := "pvst-priority"
			if v.Mode == "rstp" {
				priority = "bridge-priority"
			}
			body = map[string]any{"vlan": []any{map[string]any{"vlan-id": v.VLANID, "config": map[string]any{"vlan-id": v.VLANID, priority: v.Priority}}}}
			if current != nil {
				method = http.MethodPatch
				container := "pvst"
				if v.Mode == "rstp" {
					container = "rapid-pvst"
				}
				body = map[string]any{container: body}
			}
		} else {
			if removed[current.Mode] {
				return current, errors.New("spanning-tree VLAN deletion did not converge")
			}
			removed[current.Mode] = true
			method = http.MethodDelete
			endpoint = path.Join(stpVLANPath(current.Mode), "vlan="+strconv.FormatInt(v.VLANID, 10))
		}
		writeErr := d.rest.Do(ctx, method, endpoint, body, nil)
		observed, readErr := d.STPVLANs(ctx)
		if readErr != nil {
			return current, errors.Join(writeErr, readErr)
		}
		current = nil
		for _, entry := range observed {
			if entry.VLANID == v.VLANID {
				current = &entry
			}
		}
		for _, neighbor := range vlans {
			if neighbor.VLANID != v.VLANID && !slices.Contains(observed, neighbor) {
				return current, errors.New("spanning-tree operation changed an unrelated VLAN")
			}
		}
		if present {
			if current == nil || *current != v {
				return current, errors.Join(writeErr, errors.New("spanning-tree VLAN configuration did not converge"))
			}
		} else if current != nil && (removed[current.Mode] || current.Mode != "stp") {
			return current, errors.Join(writeErr, errors.New("spanning-tree VLAN deletion did not converge"))
		}
	}
	if d.config.Persistence == "after_each_write" {
		return current, d.save(ctx)
	}
	return current, nil
}

func stpVLANChildren(config string, v STPVLAN) error {
	if _, err := configuration(config); err != nil {
		return err
	}
	inside, found := false, false
	mode := "spanning-tree"
	if v.Mode == "rstp" {
		mode += " 802-1w"
	}
	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if !strings.HasPrefix(line, " ") && len(fields) > 1 && fields[0] == "vlan" {
			inside = fields[1] == strconv.FormatInt(v.VLANID, 10)
			continue
		}
		if line != "" && line != "!" && !strings.HasPrefix(line, " ") {
			inside = false
		}
		if !inside || len(fields) == 0 || fields[0] != "spanning-tree" {
			continue
		}
		command := strings.Join(fields, " ")
		if command == mode {
			found = true
			continue
		}
		if command == mode+" priority "+strconv.FormatInt(v.Priority, 10) {
			found = true
			continue
		}
		return errors.New("VLAN has additional spanning-tree settings; remove them before destroying its spanning-tree configuration")
	}
	if !found {
		return errors.New("spanning-tree mode was not found in native VLAN configuration")
	}
	return nil
}
