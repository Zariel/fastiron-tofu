package stp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	vlanfeature "github.com/zariel/fastiron-tofu/internal/features/vlan"
)

func applyVLAN(ctx context.Context, d *fastiron.Device, v vlan, present bool) (*vlan, error) {
	if err := validateVLAN(v); err != nil {
		return nil, err
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	var vlans []vlan
	if present {
		vlans, err = readVLANs(ctx, d)
	} else {
		vlans, err = waitVLAN(ctx, d, v.VLANID)
	}
	if err != nil {
		return nil, err
	}
	var current *vlan
	for _, entry := range vlans {
		if entry.VLANID == v.VLANID {
			current = &entry
		}
	}
	if present {
		if _, err := vlanfeature.Read(ctx, d, v.VLANID); err != nil {
			return nil, err
		}
		if current != nil && current.Mode != v.Mode {
			return nil, errors.New("VLAN uses another spanning-tree mode; import its configuration before replacing the mode")
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
		writeErr := d.DoREST(ctx, method, endpoint, body, nil)
		var observed []vlan
		var readErr error
		if present {
			observed, readErr = readVLANs(ctx, d)
		} else {
			observed, readErr = waitVLAN(ctx, d, v.VLANID)
		}
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
	return current, d.Persist(ctx)
}

func waitVLAN(ctx context.Context, d *fastiron.Device, id int64) ([]vlan, error) {
	ctx, cancel := context.WithTimeout(ctx, d.RESTCONFTimeout())
	defer cancel()

	for {
		vlans, err := readVLANs(ctx, d)
		if err != nil {
			return nil, err
		}
		output, err := d.RunningConfig(ctx)
		if err != nil {
			return vlans, err
		}
		native, err := nativeSTPVLAN(output, id)
		if err != nil {
			return vlans, err
		}
		var current *vlan
		for _, entry := range vlans {
			if entry.VLANID == id {
				current = &entry
			}
		}
		if current == nil && native == nil || current != nil && native != nil && *current == *native {
			return vlans, nil
		}

		// RSTP removal can briefly hide its classic fallback in RESTCONF. Only
		// agreeing native and RESTCONF observations establish the next mutation.
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return vlans, fmt.Errorf("native and RESTCONF spanning-tree state did not converge: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func nativeSTPVLAN(config string, id int64) (*vlan, error) {
	inside := false
	var current *vlan

	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if !strings.HasPrefix(line, " ") && len(fields) > 1 && fields[0] == "vlan" {
			inside = fields[1] == strconv.FormatInt(id, 10)
			continue
		}
		if line != "" && line != "!" && !strings.HasPrefix(line, " ") {
			inside = false
		}
		if !inside || len(fields) == 0 || fields[0] != "spanning-tree" {
			continue
		}

		fields = fields[1:]
		mode := "stp"
		if len(fields) > 0 && fields[0] == "802-1w" {
			mode = "rstp"
			fields = fields[1:]
		}
		if current == nil {
			current = &vlan{VLANID: id, Mode: mode, Priority: 32768}
		} else if current.Mode != mode {
			return nil, errors.New("native VLAN contains conflicting spanning-tree modes")
		}
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || fields[0] != "priority" {
			return nil, errors.New("VLAN has additional spanning-tree settings; remove them before destroying its spanning-tree configuration")
		}
		priority, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || priority < 0 || priority > 65535 {
			return nil, errors.New("invalid native spanning-tree bridge priority")
		}
		current.Priority = priority
	}
	return current, nil
}
