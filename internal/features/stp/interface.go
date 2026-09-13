package stp

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/features/ethernet"

	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type interfaceConfig struct {
	AdminEdge bool
	BPDUGuard bool
	RootGuard bool
}

func validateInterface(name string) error {
	if !strings.HasPrefix(name, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return errors.New("interface must be a canonical Ethernet name: ethernet <stack>/<slot>/<port>")
	}
	return nil
}

func readInterface(ctx context.Context, d *fastiron.Device, name string) (interfaceConfig, error) {
	if err := validateInterface(name); err != nil {
		return interfaceConfig{}, err
	}
	// An omitted STP entry denotes defaults only for an existing interface.
	if err := ethernet.CheckPort(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
		return interfaceConfig{}, err
	}
	interfaces, err := readInterfaces(ctx, d)
	return interfaces[name], err
}

func applyInterface(ctx context.Context, d *fastiron.Device, name string, desired interfaceConfig) (*interfaceConfig, error) {
	if err := validateInterface(name); err != nil {
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
	if err := ethernet.CheckPort(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
		return nil, err
	}
	interfaces, err := readInterfaces(ctx, d)
	if err != nil {
		return nil, err
	}
	current := interfaces[name]
	if current != desired {
		edge, guard := "EDGE_DISABLE", "NONE"
		if desired.AdminEdge {
			edge = "EDGE_ENABLE"
		}
		if desired.RootGuard {
			guard = "ROOT"
		}
		// Patch only owned leaves; deleting the interface container would erase
		// unrelated STP options and is not the native default-reset operation.
		body := map[string]any{"interfaces": map[string]any{"interface": []any{map[string]any{"name": name, "config": map[string]any{
			"name": name, "edge-port": "openconfig-spanning-tree-types:" + edge, "guard": guard, "bpdu-guard": desired.BPDUGuard,
		}}}}}
		writeErr := d.DoREST(ctx, http.MethodPatch, "/stp/interfaces", body, nil)
		observed, readErr := readInterfaces(ctx, d)
		if readErr != nil {
			return &current, errors.Join(writeErr, readErr)
		}
		current = observed[name]
		delete(interfaces, name)
		delete(observed, name)
		if !maps.Equal(interfaces, observed) {
			return &current, errors.Join(writeErr, errors.New("spanning-tree mutation changed neighboring interfaces"))
		}
		if current != desired {
			return &current, errors.Join(writeErr, errors.New("spanning-tree interface configuration did not converge"))
		}
	}
	return &current, d.Persist(ctx)
}

func readInterfaces(ctx context.Context, d *fastiron.Device) (map[string]interfaceConfig, error) {
	if !d.RESTCONFEnabled() {
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
	if err := d.DoREST(ctx, http.MethodGet, "/stp/interfaces", nil, &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil {
		return nil, errors.New("RESTCONF spanning-tree response is missing its interface container")
	}
	interfaces := map[string]interfaceConfig{}
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
		interfaces[entry.Name] = interfaceConfig{AdminEdge: edge == "EDGE_ENABLE", BPDUGuard: config.BPDUGuard, RootGuard: guard == "ROOT"}
	}
	return interfaces, nil
}
