package stp

import (
	"context"
	"errors"
	"path"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type vlan = config.STPVLAN

func validateVLAN(v vlan) error {
	if v.VLANID < 1 || v.VLANID > 4094 {
		return errors.New("vlan_id must be between 1 and 4094")
	}
	if v.Mode != "stp" && v.Mode != "rstp" {
		return errors.New("mode must be stp or rstp")
	}
	if v.Priority < 0 || v.Priority > 65535 {
		return errors.New("priority must be between 0 and 65535")
	}
	return nil
}

func stpVLANPath(mode string) string {
	if mode == "rstp" {
		return path.Join("/stp", "rapid-pvst")
	}
	return path.Join("/stp", "icx-openconfig-spanning-tree-aug:pvst")
}

func readVLANs(ctx context.Context, d *fastiron.Device) ([]vlan, error) {
	if _, err := readRESTVLANs(ctx, d); err != nil {
		return nil, err
	}
	output, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	document, err := config.Parse(output)
	if err != nil {
		return nil, err
	}
	// Cached modes and priorities can outlive CLI changes or omit native policies.
	return document.STPVLANs()
}

func readRESTVLANs(ctx context.Context, d *fastiron.Device) ([]vlan, error) {
	if !d.RESTCONFEnabled() {
		return nil, errors.New("spanning-tree configuration currently requires RESTCONF")
	}
	type vlanCollection struct {
		VLAN []struct {
			ID     int64 `json:"vlan-id"`
			Config *struct {
				ID           *int64 `json:"vlan-id"`
				RSTPPriority *int64 `json:"bridge-priority"`
				STPPriority  *int64 `json:"pvst-priority"`
			} `json:"config"`
		} `json:"vlan"`
	}
	var response struct {
		STP *struct {
			RSTP *vlanCollection `json:"rapid-pvst"`
			STP  *vlanCollection `json:"icx-openconfig-spanning-tree-aug:pvst"`
		} `json:"openconfig-spanning-tree:stp"`
	}
	if err := d.ReadREST(ctx, "/stp", &response); err != nil {
		return nil, err
	}
	if response.STP == nil || response.STP.RSTP == nil || response.STP.STP == nil {
		return nil, errors.New("RESTCONF spanning-tree response is missing its mode collections")
	}
	vlans := []vlan{}
	ids := map[int64]bool{}
	for _, collection := range []struct {
		mode string
		data *vlanCollection
	}{{"stp", response.STP.STP}, {"rstp", response.STP.RSTP}} {
		for _, entry := range collection.data.VLAN {
			if entry.Config == nil || entry.Config.ID == nil || *entry.Config.ID != entry.ID || ids[entry.ID] {
				return nil, errors.New("RESTCONF spanning-tree VLAN has an inconsistent or duplicate identity")
			}
			v := vlan{VLANID: entry.ID, Mode: collection.mode, Priority: 32768}
			priority := entry.Config.STPPriority
			if collection.mode == "rstp" {
				priority = entry.Config.RSTPPriority
			}
			if priority != nil {
				v.Priority = *priority
			}
			if err := validateVLAN(v); err != nil {
				return nil, err
			}
			ids[v.VLANID] = true
			vlans = append(vlans, v)
		}
	}
	slices.SortFunc(vlans, func(a, b vlan) int { return int(a.VLANID - b.VLANID) })
	return vlans, nil
}
