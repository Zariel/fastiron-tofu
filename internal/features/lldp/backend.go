package lldp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

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
		if err := d.DoREST(ctx, http.MethodGet, "/lldp/config", nil, &response); err != nil {
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
		if err := d.DoREST(ctx, http.MethodGet, path.Join("/lldp/interfaces", "interface="+url.PathEscape(name)), nil, &response); err != nil {
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

// Global RESTCONF configuration can remain stale after a CLI change. Validate
// its capability container, then use native configuration for configured truth.
func readEnabled(ctx context.Context, device *fastiron.Device, name string) (bool, error) {
	cached, err := readRESTEnabled(ctx, device, name)
	if err != nil || name != "" {
		return cached, err
	}
	observed, err := readGlobalNative(ctx, device)
	return observed.enabled, err
}

func applyEnabled(ctx context.Context, d *fastiron.Device, name string, enabled bool) (*bool, error) {
	unlock, err := d.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	if name == "" {
		return applyGlobal(ctx, d, enabled)
	}
	current, err := readEnabled(ctx, d, name)
	if err != nil {
		return nil, err
	}
	if current != enabled {
		body := map[string]any{"interfaces": map[string]any{"interface": []any{map[string]any{"name": name, "config": map[string]any{"name": name, "enabled": enabled}}}}}
		writeErr := d.DoREST(ctx, http.MethodPatch, "/lldp/interfaces", body, nil)
		observed, readErr := readEnabled(ctx, d, name)
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		if observed != enabled {
			return &observed, errors.Join(writeErr, errors.New("LLDP configuration did not converge"))
		}
		if writeErr != nil {
			return &observed, writeErr
		}
		current = observed
	}
	return &current, d.Persist(ctx)
}

func readInterfaces(ctx context.Context, d *fastiron.Device) (map[string]bool, error) {
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
	if err := d.DoREST(ctx, http.MethodGet, "/lldp/interfaces", nil, &response); err != nil {
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
		if _, duplicate := interfaces[entry.Name]; duplicate {
			return nil, errors.New("RESTCONF LLDP response contains duplicate interface identities")
		}
		interfaces[entry.Name] = entry.Config.Enabled == nil || *entry.Config.Enabled
	}
	return interfaces, nil
}
