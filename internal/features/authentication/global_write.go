package authentication

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/features/vlan"
)

func validateGlobal(p globalConfig) error {
	if p.AuthOrder != "dot1x mac-auth" && p.AuthOrder != "mac-auth dot1x" {
		return errors.New("auth_order must be dot1x mac-auth or mac-auth dot1x")
	}
	for _, id := range []int64{p.DefaultVLAN, p.RestrictedVLAN, p.CriticalVLAN, p.VoiceVLAN} {
		if id < 0 || id > 4094 {
			return errors.New("authentication VLAN IDs must be between 1 and 4094, or unset")
		}
	}
	if (p.Dot1XEnabled || p.MACEnabled) && p.DefaultVLAN == 0 {
		return errors.New("auth_default_vlan is required before enabling global authentication")
	}
	if p.MaxSessions < 1 {
		return errors.New("max_sessions must be positive")
	}
	if p.FailureAction != "" && p.FailureAction != "restricted-vlan" {
		return errors.New("failure_action currently supports only restricted-vlan or unset through RESTCONF")
	}
	if p.FailureAction != "" && p.RestrictedVLAN == 0 {
		return errors.New("failure_action requires restricted_vlan")
	}
	if !slices.Contains([]string{"", "success", "failure", "critical-vlan"}, p.TimeoutAction) {
		return errors.New("timeout_action currently supports success, failure, critical-vlan or unset through RESTCONF")
	}
	if p.TimeoutAction == "critical-vlan" && p.CriticalVLAN == 0 {
		return errors.New("critical-vlan timeout_action requires critical_vlan")
	}
	return nil
}

// globalValues contains only writable global fields. Guest VLAN, timers, and
// port configuration remain outside this resource's RESTCONF ownership.
func globalValues(p globalConfig) map[string]any {
	return map[string]any{
		"auth-default-vlan": p.DefaultVLAN, "restricted-vlan": p.RestrictedVLAN,
		"critical-vlan": p.CriticalVLAN, "voice-vlan": p.VoiceVLAN,
		"max-sessions": p.MaxSessions, "re-authentication": p.Reauthentication,
		"auth-order": p.AuthOrder, "fail-action": p.FailureAction, "timeout-action": p.TimeoutAction,
		"dot1x/enable": p.Dot1XEnabled, "mac-authentication/enable": p.MACEnabled,
		"mac-authentication/dot1x-disable":  p.MACDot1XDisable,
		"mac-authentication/dot1x-override": p.MACDot1XOverride,
	}
}

func globalRequest(key string, value any) (string, string, any) {
	root := "/authentication/config"
	if value == int64(0) || value == "" || key == "auth-order" && value == "dot1x mac-auth" {
		return http.MethodDelete, path.Join(root, key), nil
	}
	if value == false && key != "dot1x/enable" && key != "mac-authentication/enable" {
		return http.MethodDelete, path.Join(root, key), nil
	}
	container, leaf := path.Dir(key), path.Base(key)
	if container != "." {
		return http.MethodPatch, path.Join(root, container), map[string]any{container: map[string]any{leaf: value}}
	}
	switch key {
	case "auth-order":
		value = map[string]string{"mac-auth": "dot1x"}
	case "fail-action":
		value = map[string]any{"fail-action": value}
	case "timeout-action":
		value = map[string]bool{value.(string): true}
	}
	return http.MethodPatch, root, map[string]any{"config": map[string]any{key: value}}
}

func applyGlobal(ctx context.Context, d *fastiron.Device, desired globalConfig) (*globalConfig, error) {
	if err := d.CheckAAAChanges(); err != nil {
		return nil, err
	}
	if err := validateGlobal(desired); err != nil {
		return nil, err
	}
	if !d.RESTCONFEnabled() {
		return nil, errors.New("global authentication configuration requires RESTCONF")
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*globalConfig, error) {
		read := func() (globalConfig, []string, string, error) {
			output, err := d.RunningConfig(ctx)
			if err != nil {
				return globalConfig{}, nil, "", err
			}
			output, err = fastiron.NormalizeConfiguration(output)
			if err != nil {
				return globalConfig{}, nil, "", err
			}
			p, unowned, err := nativeGlobal(output)
			return p, unowned, output, err
		}
		current, unowned, output, err := read()
		if err != nil {
			return nil, err
		}
		interfaces, _, err := nativeAuthenticationInterfaces(output)
		if err != nil {
			return &current, err
		}
		for name, p := range interfaces {
			if current.Dot1XEnabled && !desired.Dot1XEnabled && p.Dot1XEnabled || current.MACEnabled && !desired.MACEnabled && p.MACEnabled {
				return &current, fmt.Errorf("disable authentication on %s before disabling its global feature", name)
			}
		}
		if current.FailureAction != "" && current.FailureAction != "restricted-vlan" {
			return &current, errors.New("global failure action has unsupported voice VLAN settings")
		}
		if strings.Contains(current.TimeoutAction, " ") {
			return &current, errors.New("global timeout action has unsupported voice VLAN settings")
		}
		for _, id := range []int64{desired.DefaultVLAN, desired.RestrictedVLAN, desired.CriticalVLAN, desired.VoiceVLAN} {
			if id == 0 {
				continue
			}
			if _, err := vlan.Read(ctx, d, id); err != nil {
				return &current, fmt.Errorf("authentication VLAN %d must exist: %w", id, err)
			}
		}
		values, wanted := globalValues(current), globalValues(desired)
		write := func(key string, value any) error {
			method, endpoint, body := globalRequest(key, value)
			var writeErr error
			if method == http.MethodDelete {
				writeErr = update.DeleteIfPresent(endpoint)
			} else {
				writeErr = update.REST(method, endpoint, body)
			}
			expected := maps.Clone(values)
			expected[key] = value
			observed, neighbors, _, readErr := read()
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			current = observed
			values = globalValues(current)
			if !slices.Equal(unowned, neighbors) {
				return errors.Join(writeErr, errors.New("global authentication operation changed unrelated native configuration"))
			}
			if !maps.Equal(values, expected) {
				return errors.Join(writeErr, fmt.Errorf("global authentication did not converge at %s", endpoint))
			}
			if writeErr != nil {
				return fmt.Errorf("global authentication %s %s: %w", method, endpoint, writeErr)
			}
			return nil
		}
		// Clear changed actions before removing their VLAN prerequisites. Reapply
		// desired actions after other settings, including retries after partial failure.
		for _, key := range []string{"fail-action", "timeout-action"} {
			if values[key] == "" || values[key] == wanted[key] {
				continue
			}
			if err := write(key, ""); err != nil {
				return &current, err
			}
		}
		for _, key := range []string{"mac-authentication/dot1x-disable", "mac-authentication/dot1x-override", "dot1x/enable", "mac-authentication/enable"} {
			if values[key] == wanted[key] || wanted[key] != false {
				continue
			}
			if err := write(key, false); err != nil {
				return &current, err
			}
		}
		for _, key := range []string{"auth-default-vlan", "restricted-vlan", "critical-vlan", "voice-vlan", "max-sessions", "re-authentication", "auth-order", "dot1x/enable", "mac-authentication/enable", "mac-authentication/dot1x-disable", "mac-authentication/dot1x-override"} {
			if values[key] == wanted[key] {
				continue
			}
			if err := write(key, wanted[key]); err != nil {
				return &current, err
			}
		}
		for _, key := range []string{"fail-action", "timeout-action"} {
			if wanted[key] == "" {
				continue
			}
			if err := write(key, wanted[key]); err != nil {
				return &current, err
			}
		}
		return &current, nil
	})
}
