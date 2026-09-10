package fastiron

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

var ospfAreasPath = path.Join(protocolsPath, "protocol=OSPF,icx-ospf", "ospfv2/areas")

type (
	OSPFArea struct {
		ID         string
		Interfaces []string
	}
	ospfArea struct {
		OSPFArea
		key string
	}
)

func ValidateOSPFAreaID(id string) error {
	ip, err := netip.ParseAddr(id)
	if err != nil || !ip.Is4() || ip.String() != id {
		return errors.New("area_id must be a canonical dotted IPv4-format area identifier")
	}
	return nil
}

func ValidateOSPFInterface(name string) error {
	if lagPattern.MatchString(name) || strings.HasPrefix(name, "ethernet ") && portPattern.MatchString(strings.TrimPrefix(name, "ethernet ")) || strings.HasPrefix(name, "ve ") && ValidateAddressInterface(name) == nil {
		return nil
	}
	return errors.New("OSPF bindings require a canonical Ethernet, LAG, or VE interface name")
}

func normalizeAreaID(value string) (string, error) {
	if ValidateOSPFAreaID(value) == nil {
		return value, nil
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return "", errors.New("invalid OSPF area identifier")
	}
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], uint32(n))
	return netip.AddrFrom4(bytes).String(), nil
}

func (d *Device) OSPFAreas(ctx context.Context) ([]OSPFArea, error) {
	areas, err := d.readOSPFAreas(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	result := make([]OSPFArea, 0, len(areas))
	for _, area := range areas {
		result = append(result, area.OSPFArea)
	}
	return result, nil
}

func (d *Device) readOSPFAreas(ctx context.Context) ([]ospfArea, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("OSPF areas currently require RESTCONF")
	}
	var response struct {
		Areas *struct {
			Area []struct {
				ID     json.RawMessage `json:"identifier"`
				Config *struct {
					ID json.RawMessage `json:"identifier"`
				} `json:"config"`
				Interfaces *struct {
					Interface []struct {
						ID     string `json:"id"`
						Config *struct {
							ID string `json:"id"`
						} `json:"config"`
					} `json:"interface"`
				} `json:"interfaces"`
			} `json:"area"`
		} `json:"openconfig-network-instance:areas"`
	}
	err := d.rest.Do(ctx, http.MethodGet, ospfAreasPath, nil, &response)
	if errors.Is(err, restconf.ErrNotFound) {
		var parent struct {
			Protocols *struct {
				Protocol []struct {
					Identifier string `json:"identifier"`
					Name       string `json:"name"`
				} `json:"protocol"`
			} `json:"openconfig-network-instance:protocols"`
		}
		if parentErr := d.rest.Do(ctx, http.MethodGet, protocolsPath, nil, &parent); parentErr != nil {
			return nil, parentErr
		}
		if parent.Protocols == nil {
			return nil, errors.New("RESTCONF protocol response is missing its collection")
		}
		for _, protocol := range parent.Protocols.Protocol {
			if protocol.Identifier == "" || protocol.Name == "" {
				return nil, errors.New("RESTCONF protocol response is missing an identity")
			}
			if protocol.Name == "icx-ospf" {
				return nil, err
			}
		}
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if response.Areas == nil {
		return nil, errors.New("RESTCONF OSPF response is missing its area collection")
	}
	areas := []ospfArea{}
	ids := map[string]bool{}
	bindings := map[string]bool{}
	for _, entry := range response.Areas.Area {
		key := strings.Trim(string(entry.ID), `"`)
		id, err := normalizeAreaID(key)
		if err != nil || entry.Config == nil || entry.Interfaces == nil {
			return nil, errors.New("RESTCONF OSPF area is missing its identity or interface collection")
		}
		configured, err := normalizeAreaID(strings.Trim(string(entry.Config.ID), `"`))
		if err != nil || configured != id || ids[id] {
			return nil, errors.New("RESTCONF OSPF area has an inconsistent or duplicate identity")
		}
		ids[id] = true
		area := ospfArea{OSPFArea: OSPFArea{ID: id, Interfaces: []string{}}, key: key}
		for _, binding := range entry.Interfaces.Interface {
			if binding.Config == nil || binding.Config.ID != binding.ID || ValidateOSPFInterface(binding.ID) != nil {
				return nil, errors.New("RESTCONF OSPF binding has an incomplete or unsupported interface identity")
			}
			if bindings[binding.ID] {
				return nil, errors.New("RESTCONF OSPF response binds an interface more than once")
			}
			bindings[binding.ID] = true
			area.Interfaces = append(area.Interfaces, binding.ID)
		}
		slices.Sort(area.Interfaces)
		areas = append(areas, area)
	}
	slices.SortFunc(areas, func(a, b ospfArea) int { return strings.Compare(a.ID, b.ID) })
	return areas, nil
}
