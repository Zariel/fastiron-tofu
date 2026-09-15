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

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/features/ethernet"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
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
	if err := ethernet.CheckPort(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
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
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*interfaceConfig, error) {
		if err := ethernet.CheckPort(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
			return nil, err
		}

		read := func() (map[string]interfaceConfig, []string, *nativeconfig.Document, error) {
			document, err := d.RunningConfig(ctx)
			if err != nil {
				return nil, nil, nil, err
			}
			interfaces, unowned, err := nativeAuthenticationInterfaces(document)
			if err != nil {
				return nil, nil, nil, err
			}
			neighbors, err := authenticationNeighbors(unowned, name, desired.Dot1XEnabled || desired.MACEnabled)
			return interfaces, neighbors, document, err
		}
		interfaces, unowned, document, err := read()
		if err != nil {
			return nil, err
		}
		current, ok := interfaces[name]
		if !ok {
			current.PortControl = "force-authorized"
		}
		neighbors := maps.Clone(interfaces)
		delete(neighbors, name)

		dot1xEnabled, macEnabled := false, false
		for _, command := range document.Commands {
			if command.Parent < 0 || document.Commands[command.Parent].Text != "authentication" {
				continue
			}
			switch strings.Join(command.Fields, " ") {
			case "dot1x enable":
				dot1xEnabled = true
			case "mac-authentication enable":
				macEnabled = true
			}
		}
		if desired.Dot1XEnabled && !dot1xEnabled || desired.MACEnabled && !macEnabled {
			return &current, errors.New("enable the corresponding global authentication feature before enabling a port")
		}

		actions, actionErr := globalActions(unowned)
		if actionErr != nil && (current.Dot1XEnabled != desired.Dot1XEnabled || current.MACEnabled != desired.MACEnabled) {
			return &current, actionErr
		}

		// Port requests own one port; action reapplication preserves global policy.
		// Native verification stops dependent writes after unrelated or ambiguous changes.
		write := func(method, endpoint string, body any, expected *interfaceConfig, allowMissing bool) error {
			var writeErr error
			if allowMissing {
				writeErr = update.DeleteIfPresent(endpoint)
			} else {
				writeErr = update.REST(method, endpoint, body)
			}
			if writeErr != nil {
				writeErr = fmt.Errorf("authentication %s %s: %w", method, endpoint, writeErr)
			}
			observed, after, _, readErr := read()
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

		if actionErr == nil && len(actions) > 0 {
			// Repeat on retries even if a previous request already changed the flags.
			// Unchanged global configuration cannot prove that reapplication succeeded.
			expected := current
			if err := write(http.MethodPatch, root, map[string]any{"config": actions}, &expected, false); err != nil {
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
		return &current, nil
	})
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
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			vlan = ""
		}
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
