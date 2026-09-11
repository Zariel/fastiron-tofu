package fastiron

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type STPInterface struct {
	AdminEdge bool
	BPDUGuard bool
	RootGuard bool
}

func (d *Device) STPInterfaces(ctx context.Context) (map[string]STPInterface, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("spanning-tree configuration currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name   string `json:"name"`
				Config *struct {
					Name      string `json:"name"`
					EdgePort  string `json:"edge-port"`
					Guard     string `json:"guard"`
					BPDUGuard bool   `json:"bpdu-guard"`
				} `json:"config"`
			} `json:"interface"`
		} `json:"openconfig-spanning-tree:interfaces"`
	}
	if err := d.rest.Do(ctx, http.MethodGet, "/stp/interfaces", nil, &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil {
		return nil, errors.New("RESTCONF spanning-tree response is missing its interface container")
	}
	interfaces := map[string]STPInterface{}
	for _, entry := range response.Interfaces.Interface {
		if entry.Name == "" || entry.Config == nil || entry.Config.Name != entry.Name {
			return nil, errors.New("RESTCONF spanning-tree interface contains an inconsistent identity")
		}
		if _, duplicate := interfaces[entry.Name]; duplicate {
			return nil, errors.New("RESTCONF spanning-tree response contains duplicate interface identities")
		}
		config := entry.Config
		edge := strings.TrimPrefix(config.EdgePort, "openconfig-spanning-tree-types:")
		guard := strings.TrimPrefix(config.Guard, "openconfig-spanning-tree-types:")
		if edge != "" && edge != "EDGE_ENABLE" && edge != "EDGE_DISABLE" {
			return nil, errors.New("RESTCONF spanning-tree interface has an unsupported edge-port setting")
		}
		if guard != "" && guard != "NONE" && guard != "ROOT" {
			return nil, errors.New("RESTCONF spanning-tree interface has an unsupported guard setting")
		}
		// FastIron omits default-disabled options until they are explicitly set.
		interfaces[entry.Name] = STPInterface{AdminEdge: edge == "EDGE_ENABLE", BPDUGuard: config.BPDUGuard, RootGuard: guard == "ROOT"}
	}
	return interfaces, nil
}
