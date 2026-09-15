package lldp

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

func validateInterface(name string) error {
	if !strings.HasPrefix(name, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return errors.New("interface must be a canonical Ethernet name: ethernet <stack>/<slot>/<port>")
	}
	return nil
}

type lldpConfig struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled"`
}

// readRESTEnabled reads the global setting when name is empty, otherwise the named interface.
func readRESTEnabled(ctx context.Context, d *fastiron.Device, name string) (bool, error) {
	if name != "" {
		if err := validateInterface(name); err != nil {
			return false, err
		}
	}
	if !d.RESTCONFEnabled() {
		return false, errors.New("LLDP configuration currently requires RESTCONF")
	}
	var config *lldpConfig
	if name == "" {
		var response struct {
			Config *lldpConfig `json:"openconfig-lldp:config"`
		}
		if err := d.ReadREST(ctx, "/lldp/config", &response); err != nil {
			return false, err
		}
		config = response.Config
	} else {
		var response struct {
			Interfaces []struct {
				Name   string      `json:"name"`
				Config *lldpConfig `json:"config"`
			} `json:"openconfig-lldp:interface"`
		}
		if err := d.ReadREST(ctx, path.Join("/lldp/interfaces", "interface="+url.PathEscape(name)), &response); err != nil {
			return false, err
		}
		if len(response.Interfaces) != 1 || response.Interfaces[0].Name != name {
			return false, errors.New("RESTCONF LLDP response is missing the requested interface")
		}
		config = response.Interfaces[0].Config
		if config != nil && config.Name != name {
			return false, errors.New("RESTCONF LLDP response contains an inconsistent interface identity")
		}
	}
	if config == nil {
		return false, errors.New("RESTCONF LLDP response is missing its configuration container")
	}
	// OpenConfig defaults this leaf to true. FastIron omits it after a global
	// leaf deletion; an absent container still means an unsupported response.
	return config.Enabled == nil || *config.Enabled, nil
}

// RESTCONF configuration can remain stale after CLI changes. Validate its
// capability containers, then use native configuration for configured truth.
func readEnabled(ctx context.Context, device *fastiron.Device, name string) (bool, error) {
	cached, err := readRESTEnabled(ctx, device, name)
	if err != nil {
		return cached, err
	}
	if name != "" {
		interfaces, err := readInterfaces(ctx, device)
		if err != nil {
			return false, err
		}
		enabled, exists := interfaces[name]
		if !exists {
			return false, errors.New("LLDP inventory omits the requested interface")
		}
		return enabled, nil
	}
	observed, err := readGlobalNative(ctx, device)
	return observed.enabled, err
}

func applyEnabled(ctx context.Context, d *fastiron.Device, name string, enabled bool) (*bool, error) {
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*bool, error) {
		if name == "" {
			return applyGlobal(ctx, d, update, enabled)
		}
		cached, err := readRESTEnabled(ctx, d, name)
		if err != nil {
			return nil, err
		}
		before, unowned, err := readPortModes(ctx, d)
		if err != nil {
			return nil, err
		}
		mode, exists := before[name]
		if !exists {
			return nil, errors.New("LLDP inventory omits the requested interface")
		}
		current := mode.Receive || mode.Transmit
		if current == enabled {
			return &current, nil
		}
		targets := []bool{enabled}
		if cached != current {
			// FastIron skips a native update when the requested value is cached.
			// Priming true enables both directions on the owned port, even when
			// only one direction was enabled; verify it before the desired write.
			targets = []bool{current, enabled}
		}
		delete(before, name)
		for _, target := range targets {
			body := map[string]any{"interfaces": map[string]any{"interface": []any{map[string]any{"name": name, "config": map[string]any{"name": name, "enabled": target}}}}}
			writeErr := update.REST(http.MethodPatch, "/lldp/interfaces", body)
			after, remaining, readErr := readPortModes(ctx, d)
			if readErr != nil {
				return nil, errors.Join(writeErr, readErr)
			}
			mode, exists := after[name]
			if !exists {
				return nil, errors.Join(writeErr, errors.New("LLDP inventory omits the requested interface after mutation"))
			}
			observed := mode.Receive || mode.Transmit
			delete(after, name)
			// Native range regrouping is allowed; other ports' directional state
			// and independently owned commands must survive before saving.
			if !maps.Equal(before, after) || !slices.Equal(unowned, remaining) {
				return &observed, errors.Join(writeErr, errors.New("LLDP port mutation changed unrelated configuration"))
			}
			if observed != target {
				return &observed, errors.Join(writeErr, errors.New("LLDP configuration did not converge"))
			}
			if writeErr != nil {
				return &observed, writeErr
			}
			current = observed
			cached, err = readRESTEnabled(ctx, d, name)
			if err != nil {
				return &current, err
			}
			if cached != current {
				return &current, errors.New("RESTCONF LLDP port configuration did not converge")
			}
		}
		return &current, nil
	})
}

func readRESTInterfaces(ctx context.Context, d *fastiron.Device) (map[string]bool, error) {
	if !d.RESTCONFEnabled() {
		return nil, errors.New("LLDP discovery currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name   string      `json:"name"`
				Config *lldpConfig `json:"config"`
			} `json:"interface"`
		} `json:"openconfig-lldp:interfaces"`
	}
	if err := d.ReadREST(ctx, "/lldp/interfaces", &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil {
		return nil, errors.New("RESTCONF LLDP response is missing its interface container")
	}
	interfaces := map[string]bool{}
	for _, entry := range response.Interfaces.Interface {
		if entry.Name == "" || entry.Config == nil || entry.Config.Name != entry.Name {
			return nil, errors.New("RESTCONF LLDP interface contains an inconsistent identity")
		}
		if err := validateInterface(entry.Name); err != nil {
			return nil, err
		}
		if _, duplicate := interfaces[entry.Name]; duplicate {
			return nil, errors.New("RESTCONF LLDP response contains duplicate interface identities")
		}
		interfaces[entry.Name] = entry.Config.Enabled == nil || *entry.Config.Enabled
	}
	return interfaces, nil
}

func readPortModes(ctx context.Context, device *fastiron.Device) (map[string]config.LLDPMode, []string, error) {
	inventory, err := readRESTInterfaces(ctx, device)
	if err != nil {
		return nil, nil, err
	}
	document, err := device.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}

	names := make([]string, 0, len(inventory))
	for name := range inventory {
		names = append(names, name)
	}
	return document.LLDPPorts(names)
}

func readInterfaces(ctx context.Context, device *fastiron.Device) (map[string]bool, error) {
	modes, _, err := readPortModes(ctx, device)
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]bool, len(modes))
	for name, mode := range modes {
		enabled[name] = mode.Receive || mode.Transmit
	}
	return enabled, nil
}
