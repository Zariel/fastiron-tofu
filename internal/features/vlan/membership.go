package vlan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type membership struct {
	VLANID             int64
	Interface, Tagging string
}

type switchport struct {
	Access int64   `json:"access-vlan"`
	Trunks []int64 `json:"trunk-vlans"`
}

func validateMembership(v membership) error {
	if err := fastiron.ValidateVLAN(fastiron.VLAN{ID: v.VLANID}); err != nil {
		return err
	}
	if !interfaceid.LAG(v.Interface) && (!strings.HasPrefix(v.Interface, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(v.Interface, "ethernet "))) {
		return errors.New("interface must be a canonical Ethernet or LAG name: ethernet <stack>/<slot>/<port> or lag <id>")
	}
	if v.Tagging != "tagged" && v.Tagging != "untagged" {
		return errors.New("tagging must be tagged or untagged")
	}
	return nil
}

func membershipPath(name string) string {
	if interfaceid.LAG(name) {
		return path.Join("/interfaces", "interface="+url.PathEscape(name), "aggregation/switched-vlan")
	}
	return path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/switched-vlan")
}

func ReadSwitchport(ctx context.Context, d *fastiron.Device, name string) (switchport, error) {
	if !d.RESTCONFEnabled() {
		return switchport{}, errors.New("VLAN membership currently requires RESTCONF")
	}
	var response struct {
		Port *struct {
			Config *switchport `json:"config"`
		} `json:"openconfig-vlan:switched-vlan"`
	}
	if err := d.DoREST(ctx, http.MethodGet, membershipPath(name), nil, &response); err != nil {
		return switchport{}, err
	}
	if response.Port == nil || response.Port.Config == nil {
		return switchport{}, errors.New("RESTCONF switchport response is missing its configuration container")
	}
	port := *response.Port.Config
	if port.Access < 0 || port.Access > 4094 {
		return switchport{}, errors.New("RESTCONF switchport contains an invalid access VLAN")
	}
	for _, id := range port.Trunks {
		if id < 1 || id > 4094 {
			return switchport{}, errors.New("RESTCONF switchport contains an invalid tagged VLAN")
		}
	}
	return port, nil
}

func (p switchport) contains(v membership) bool {
	if v.Tagging == "untagged" {
		return p.Access == v.VLANID
	}
	return slices.Contains(p.Trunks, v.VLANID)
}

func readMembership(ctx context.Context, d *fastiron.Device, v membership) (bool, error) {
	if err := validateMembership(v); err != nil {
		return false, err
	}
	port, err := ReadSwitchport(ctx, d, v.Interface)
	return port.contains(v), err
}

// applyMembership changes one relationship and reports observed existence,
// including when saving fails after the running configuration has converged.
func applyMembership(ctx context.Context, d *fastiron.Device, v membership, present bool) (bool, error) {
	if err := validateMembership(v); err != nil {
		return false, err
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return false, err
	}
	port, err := ReadSwitchport(ctx, d, v.Interface)
	if err != nil {
		return false, err
	}
	exists := port.contains(v)
	if exists != present {
		if present {
			if _, err := d.VLAN(ctx, v.VLANID); err != nil {
				return exists, fmt.Errorf("membership requires an existing VLAN: %w", err)
			}
			if v.Tagging == "untagged" && port.Access > 1 && port.Access != v.VLANID {
				return exists, errors.New("interface already belongs to another untagged VLAN; remove that membership first")
			}
			if (v.Tagging == "tagged" && port.Access == v.VLANID) || (v.Tagging == "untagged" && slices.Contains(port.Trunks, v.VLANID)) {
				return exists, errors.New("interface already belongs to this VLAN with different tagging; remove that membership first")
			}
		}
		endpoint := membershipPath(v.Interface)
		method := http.MethodDelete
		var body any
		if present {
			method = http.MethodPatch
			config := map[string]any{"access-vlan": v.VLANID}
			if v.Tagging == "tagged" {
				config = map[string]any{"trunk-vlans": []int64{v.VLANID}}
			}
			// Native PATCH merges leaf-list entries. Keyed DELETE removes just this
			// relationship, avoiding ownership of neighboring memberships.
			body = map[string]any{"openconfig-vlan:switched-vlan": map[string]any{"config": config}}
		} else if v.Tagging == "tagged" {
			endpoint = path.Join(endpoint, "config", fmt.Sprintf("trunk-vlans=%d", v.VLANID))
		} else {
			endpoint = path.Join(endpoint, "config/access-vlan")
		}
		writeErr := d.DoREST(ctx, method, endpoint, body, nil)
		observed, readErr := ReadSwitchport(ctx, d, v.Interface)
		if readErr != nil {
			return exists, errors.Join(writeErr, readErr)
		}
		exists = observed.contains(v)
		if exists != present {
			return exists, errors.Join(writeErr, errors.New("VLAN membership did not converge"))
		}
		// Verify that the single-relationship operation preserved every neighbor.
		if v.Tagging == "tagged" && observed.Access != port.Access {
			return exists, errors.New("tagged membership operation changed the access VLAN")
		}
		for _, id := range port.Trunks {
			if id != v.VLANID && !slices.Contains(observed.Trunks, id) {
				return exists, errors.New("membership operation removed an unrelated tagged VLAN")
			}
		}
	}
	return exists, d.Persist(ctx)
}
