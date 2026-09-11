package authentication

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/features/ethernet"

	"github.com/zariel/fastiron-tofu/internal/interfaceid"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func validateInterface(name string, desired interfaceConfig) error {
	if !strings.HasPrefix(name, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return errors.New("interface must be a canonical Ethernet name: ethernet <stack>/<slot>/<port>")
	}
	if !slices.Contains([]string{"auto", "force-authorized", "force-unauthorized"}, desired.PortControl) {
		return errors.New("port_control must be auto, force-authorized or force-unauthorized")
	}
	if !desired.Dot1XEnabled && desired.PortControl != "force-authorized" {
		return errors.New("nondefault port_control requires dot1x_enabled = true")
	}
	return nil
}

func readInterface(ctx context.Context, d *fastiron.Device, name string) (interfaceConfig, error) {
	defaults := interfaceConfig{PortControl: "force-authorized"}
	if err := validateInterface(name, defaults); err != nil {
		return defaults, err
	}
	if _, err := d.Discover(ctx); err != nil {
		return defaults, err
	}
	// Native omission denotes defaults only for an existing Ethernet interface.
	if _, err := ethernet.Read(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
		return defaults, err
	}
	interfaces, err := readInterfaces(ctx, d)
	if current, ok := interfaces[name]; ok {
		return current, err
	}
	return defaults, err
}

func applyInterface(ctx context.Context, d *fastiron.Device, name string, desired interfaceConfig) (*interfaceConfig, error) {
	if err := d.CheckAAAChanges(); err != nil {
		return nil, err
	}
	if err := validateInterface(name, desired); err != nil {
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
	if _, err := ethernet.Read(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
		return nil, err
	}

	read := func() (map[string]interfaceConfig, []string, error) {
		output, err := d.RunningConfig(ctx)
		if err != nil {
			return nil, nil, err
		}
		interfaces, unowned, err := nativeAuthenticationInterfaces(output)
		if err != nil {
			return nil, nil, err
		}
		neighbors, err := authenticationNeighbors(unowned, name, desired.Dot1XEnabled || desired.MACEnabled)
		return interfaces, neighbors, err
	}
	interfaces, unowned, err := read()
	if err != nil {
		return nil, err
	}
	current, ok := interfaces[name]
	if !ok {
		current.PortControl = "force-authorized"
	}
	neighbors := maps.Clone(interfaces)
	delete(neighbors, name)
	if desired.Dot1XEnabled && !slices.Contains(unowned, "dot1x enable") || desired.MACEnabled && !slices.Contains(unowned, "mac-authentication enable") {
		return &current, errors.New("enable the corresponding global authentication feature before enabling a port")
	}

	if current.Dot1XEnabled != desired.Dot1XEnabled || current.MACEnabled != desired.MACEnabled {
		for _, line := range unowned {
			// FastIron requires reapplying this global action after port enablement
			// changes. Preserve its behavior until that RESTCONF sequence is supported.
			if strings.HasPrefix(line, "auth-fail-action ") || strings.HasPrefix(line, "auth-timeout-action ") {
				return &current, errors.New("port enablement changes with a global authentication failure or timeout action require reapplying that action; this combination is not yet supported")
			}
		}
	}

	// Each request owns only one port. Native verification catches projection-only
	// success, and stops dependent writes after an ambiguous or unrelated change.
	write := func(method, endpoint string, body any, expected *interfaceConfig, allowMissing bool) error {
		writeErr := d.DoREST(ctx, method, endpoint, body, nil)
		if allowMissing && errors.Is(writeErr, restconf.ErrNotFound) {
			writeErr = nil
		}
		if writeErr != nil {
			writeErr = fmt.Errorf("authentication %s %s: %w", method, endpoint, writeErr)
		}
		observed, after, readErr := read()
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
		previous := current
		current, ok = observed[name]
		if !ok {
			current.PortControl = "force-authorized"
		}
		delete(observed, name)
		if !slices.Equal(unowned, after) || !maps.Equal(neighbors, observed) {
			return errors.Join(writeErr, errors.New("authentication operation changed unrelated native configuration"))
		}
		if expected != nil && current != *expected {
			return errors.Join(writeErr, errors.New("authentication interface did not converge at "+endpoint))
		}
		if expected == nil && (current.Dot1XEnabled != previous.Dot1XEnabled || current.MACEnabled != previous.MACEnabled) {
			return errors.Join(writeErr, errors.New("port-control reset changed interface enablement"))
		}
		return writeErr
	}
	root := "/authentication/config"
	key := url.PathEscape(name)
	for _, family := range []string{"dot1x", "mac-authentication"} {
		enabled, wanted := current.Dot1XEnabled, desired.Dot1XEnabled
		if family == "mac-authentication" {
			enabled, wanted = current.MACEnabled, desired.MACEnabled
		}
		if enabled == wanted {
			continue
		}
		expected := current
		if family == "dot1x" {
			expected.Dot1XEnabled = wanted
			// Native dot1x removal also resets port-control. Reconcile the desired
			// control mode after enablement changes have completed.
			if !wanted {
				expected.PortControl = "force-authorized"
			}
		} else {
			expected.MACEnabled = wanted
		}
		method, endpoint := http.MethodDelete, path.Join(root, family, "ethernet="+key)
		var body any
		if wanted {
			method, endpoint = http.MethodPatch, path.Join(root, family)
			body = map[string]any{family: map[string]string{"ethernet": name}}
		}
		if err := write(method, endpoint, body, &expected, !wanted); err != nil {
			return &current, err
		}
	}
	if current.PortControl != desired.PortControl {
		endpoint := path.Join(root, "dot1x/port-control")
		// A stale desired-mode entry can suppress PATCH. Clear only that port's
		// desired entry before applying; 404 is inconclusive until native readback.
		if err := write(http.MethodDelete, path.Join(endpoint, desired.PortControl+"="+key), nil, nil, true); err != nil {
			return &current, err
		}
		expected := current
		expected.PortControl = desired.PortControl
		body := map[string]any{"port-control": map[string]string{desired.PortControl: name}}
		if err := write(http.MethodPatch, endpoint, body, &expected, false); err != nil {
			return &current, err
		}
	}
	return &current, d.Persist(ctx)
}

// Authentication moves a port out of the system default VLAN. Exclude only
// that port's implicit membership from comparison; retain every other port and
// explicit membership, which remains outside authentication ownership.
func authenticationNeighbors(lines []string, name string, enabling bool) ([]string, error) {
	defaultVLAN := "1"
	for _, line := range lines {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "default-vlan-id" {
			defaultVLAN = f[1]
		}
	}
	var neighbors []string
	vlan := ""
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "vlan" {
			vlan = f[1]
		}
		if line == "!" {
			vlan = ""
		}
		if vlan != "" && len(f) > 1 && f[0] == "untagged" && enabling {
			if f[1] == "lag" && !slices.Contains(f, "ethe") && !slices.Contains(f, "ethernet") {
				neighbors = append(neighbors, line)
				continue
			}
			ports, err := authenticationPorts(f[1:])
			if err != nil {
				return nil, err
			}
			if slices.Contains(ports, name) {
				return nil, errors.New("remove the port's explicit untagged VLAN membership before enabling authentication")
			}
		}
		if vlan == defaultVLAN && len(f) > 2 && f[0] == "no" && f[1] == "untagged" {
			ports, err := authenticationPorts(f[2:])
			if err != nil {
				return nil, err
			}
			slices.Sort(ports)
			for _, port := range ports {
				if port != name {
					neighbors = append(neighbors, "no untagged "+port)
				}
			}
			continue
		}
		neighbors = append(neighbors, line)
	}
	return neighbors, nil
}
