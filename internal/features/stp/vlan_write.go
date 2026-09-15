package stp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strconv"
	"time"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	vlanfeature "github.com/zariel/fastiron-tofu/internal/features/vlan"
)

func applyVLAN(ctx context.Context, d *fastiron.Device, v vlan, present bool) (*vlan, error) {
	if err := validateVLAN(v); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*vlan, error) {
		var vlans []vlan
		var err error
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
			writeErr := update.REST(method, endpoint, body)
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
		return current, nil
	})
}

func waitVLAN(ctx context.Context, d *fastiron.Device, id int64) ([]vlan, error) {
	ctx, cancel := context.WithTimeout(ctx, d.RESTCONFTimeout())
	defer cancel()

	for {
		vlans, err := readRESTVLANs(ctx, d)
		if err != nil {
			return nil, err
		}
		output, err := d.RunningConfig(ctx)
		if err != nil {
			return vlans, err
		}
		document, err := nativeconfig.Parse(output)
		if err != nil {
			return vlans, err
		}
		native, err := document.STPVLAN(id)
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
			return document.STPVLANs()
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
