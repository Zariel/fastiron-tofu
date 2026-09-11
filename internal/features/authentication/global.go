package authentication

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type globalConfig struct {
	Dot1XEnabled     bool
	MACEnabled       bool
	AuthOrder        string
	DefaultVLAN      int64
	RestrictedVLAN   int64
	CriticalVLAN     int64
	VoiceVLAN        int64
	GuestVLAN        int64
	MaxSessions      int64
	Reauthentication bool
	MACDot1XDisable  bool
	MACDot1XOverride bool
	FailureAction    string
	TimeoutAction    string
}

func readGlobal(ctx context.Context, d *fastiron.Device) (globalConfig, error) {
	if _, err := d.Discover(ctx); err != nil {
		return globalConfig{}, err
	}
	output, err := d.RunningConfig(ctx)
	if err != nil {
		return globalConfig{}, err
	}
	current, _, err := nativeGlobal(output)
	return current, err
}

// RESTCONF can retain obsolete leaves after changes. Read native configuration
// to distinguish configured policy from that projection and operational defaults.
func nativeGlobal(output string) (globalConfig, []string, error) {
	var unowned []string
	result := globalConfig{AuthOrder: "dot1x mac-auth", MaxSessions: 2}
	active := false
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			active = line == "authentication"
			if !active {
				unowned = append(unowned, line)
			}
			continue
		}
		if !active {
			unowned = append(unowned, line)
			continue
		}
		key := fields[0]
		if key == "dot1x" || key == "mac-authentication" {
			if len(fields) < 2 {
				return result, nil, errors.New("incomplete native authentication command")
			}
			key += " " + fields[1]
			fields = fields[1:]
			// Port enablement and port-control belong to interface resources.
			if fields[0] == "port-control" || fields[0] == "enable" && len(fields) > 1 {
				unowned = append(unowned, line)
				continue
			}
		}
		value := strings.Join(fields[1:], " ")
		var number *int64
		var flag *bool
		switch key {
		case "auth-order":
			if value != "dot1x mac-auth" && value != "mac-auth dot1x" {
				return result, nil, errors.New("unsupported native authentication order")
			}
			result.AuthOrder = value
		case "auth-default-vlan":
			number = &result.DefaultVLAN
		case "restricted-vlan":
			number = &result.RestrictedVLAN
		case "critical-vlan":
			number = &result.CriticalVLAN
		case "voice-vlan":
			number = &result.VoiceVLAN
		case "dot1x guest-vlan":
			number = &result.GuestVLAN
			// Guest VLAN has no verified scoped RESTCONF reset and remains read-only.
			unowned = append(unowned, line)
		case "max-sessions":
			number = &result.MaxSessions
		case "re-authentication":
			flag = &result.Reauthentication
		case "dot1x enable":
			flag = &result.Dot1XEnabled
		case "mac-authentication enable":
			flag = &result.MACEnabled
		case "mac-authentication dot1x-disable":
			flag = &result.MACDot1XDisable
		case "mac-authentication dot1x-override":
			flag = &result.MACDot1XOverride
		case "auth-fail-action":
			if value != "restricted-vlan" && value != "restricted-vlan voice voice-vlan" {
				return result, nil, errors.New("unsupported native authentication failure action")
			}
			result.FailureAction = value
		case "auth-timeout-action":
			if value != "success" && value != "failure" && value != "critical-vlan" && value != "critical-vlan voice voice-vlan" {
				return result, nil, errors.New("unsupported native authentication timeout action")
			}
			result.TimeoutAction = value
		default:
			unowned = append(unowned, line)
			continue
		}
		if seen[key] {
			return result, nil, errors.New("duplicate native authentication setting: " + key)
		}
		seen[key] = true
		if number != nil {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed < 1 {
				return result, nil, errors.New("invalid native authentication numeric setting: " + key)
			}
			*number = parsed
		}
		if flag != nil {
			if value != "" {
				return result, nil, errors.New("invalid native authentication flag: " + key)
			}
			*flag = true
		}
	}
	return result, unowned, nil
}
