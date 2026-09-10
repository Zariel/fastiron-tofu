package fastiron

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
)

type STPVLAN struct {
	VLANID   int64
	Mode     string
	Priority int64
}

func ValidateSTPVLAN(v STPVLAN) error {
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

func (d *Device) STPVLANs(ctx context.Context) ([]STPVLAN, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
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
	if err := d.rest.Do(ctx, http.MethodGet, "/stp", nil, &response); err != nil {
		return nil, err
	}
	if response.STP == nil || response.STP.RSTP == nil || response.STP.STP == nil {
		return nil, errors.New("RESTCONF spanning-tree response is missing its mode collections")
	}
	vlans := []STPVLAN{}
	ids := map[int64]bool{}
	for _, collection := range []struct {
		mode string
		data *vlanCollection
	}{{"stp", response.STP.STP}, {"rstp", response.STP.RSTP}} {
		for _, entry := range collection.data.VLAN {
			if entry.Config == nil || entry.Config.ID == nil || *entry.Config.ID != entry.ID || ids[entry.ID] {
				return nil, errors.New("RESTCONF spanning-tree VLAN has an inconsistent or duplicate identity")
			}
			v := STPVLAN{VLANID: entry.ID, Mode: collection.mode, Priority: 32768}
			priority := entry.Config.STPPriority
			if collection.mode == "rstp" {
				priority = entry.Config.RSTPPriority
			}
			if priority != nil {
				v.Priority = *priority
			}
			if err := ValidateSTPVLAN(v); err != nil {
				return nil, err
			}
			ids[v.VLANID] = true
			vlans = append(vlans, v)
		}
	}
	slices.SortFunc(vlans, func(a, b STPVLAN) int { return int(a.VLANID - b.VLANID) })
	return vlans, nil
}
