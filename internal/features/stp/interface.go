package stp

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/features/ethernet"

	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type interfaceConfig = config.STPInterface

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
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*interfaceConfig, error) {
		if err := ethernet.CheckPort(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
			return nil, err
		}
		cached, err := readRESTInterfaces(ctx, d)
		if err != nil {
			return nil, err
		}
		current, unowned, err := readNativeInterface(ctx, d, name)
		if err != nil {
			return nil, err
		}
		if current == desired {
			return &current, nil
		}
		targets := []interfaceConfig{desired}
		if cached[name] != current {
			// Reapply current flags to align a stale cache without changing native policy.
			targets = []interfaceConfig{current, desired}
		}
		for i, target := range targets {
			edge, guard := "EDGE_DISABLE", "NONE"
			if target.AdminEdge {
				edge = "EDGE_ENABLE"
			}
			if target.RootGuard {
				guard = "ROOT"
			}
			// Collection PUT can erase neighboring interfaces. PATCH only owned leaves.
			body := map[string]any{"interfaces": map[string]any{"interface": []any{map[string]any{"name": name, "config": map[string]any{
				"name": name, "edge-port": "openconfig-spanning-tree-types:" + edge, "guard": guard, "bpdu-guard": target.BPDUGuard,
			}}}}}
			writeErr := update.REST(http.MethodPatch, "/stp/interfaces", body)
			observed, remaining, readErr := readNativeInterface(ctx, d, name)
			if readErr != nil {
				return &current, errors.Join(writeErr, readErr)
			}
			current = observed
			if !slices.Equal(unowned, remaining) {
				return &current, errors.Join(writeErr, errors.New("spanning-tree mutation changed unrelated configuration"))
			}
			if current != target {
				return &current, errors.Join(writeErr, errors.New("spanning-tree interface configuration did not converge"))
			}
			if writeErr != nil {
				return &current, writeErr
			}
			if i+1 < len(targets) {
				if err := waitInterfaceCache(ctx, d, name, current); err != nil {
					return &current, err
				}
			}
		}
		return &current, nil
	})
}

func readNativeInterface(ctx context.Context, d *fastiron.Device, name string) (interfaceConfig, []string, error) {
	output, err := d.RunningConfig(ctx)
	if err != nil {
		return interfaceConfig{}, nil, err
	}
	document, err := config.Parse(output)
	if err != nil {
		return interfaceConfig{}, nil, err
	}
	return document.STPInterface(name)
}

func waitInterfaceCache(ctx context.Context, d *fastiron.Device, name string, current interfaceConfig) error {
	ctx, cancel := context.WithTimeout(ctx, d.RESTCONFTimeout())
	defer cancel()
	for {
		cached, err := readRESTInterfaces(ctx, d)
		if err != nil {
			return err
		}
		if cached[name] == current {
			return nil
		}
		// RESTCONF can lag native changes; a following PATCH must see the aligned values.
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(errors.New("RESTCONF spanning-tree cache did not synchronize with native flags"), ctx.Err())
		case <-timer.C:
		}
	}
}

func readInterfaces(ctx context.Context, d *fastiron.Device) (map[string]interfaceConfig, error) {
	if _, err := readRESTInterfaces(ctx, d); err != nil {
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
	// Cache-only identities disappear across reboot without any native policy change.
	return document.STPInterfaces()
}

func readRESTInterfaces(ctx context.Context, d *fastiron.Device) (map[string]interfaceConfig, error) {
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
	if err := d.ReadREST(ctx, "/stp/interfaces", &response); err != nil {
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
