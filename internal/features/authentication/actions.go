package authentication

import (
	"errors"
	"strings"
)

func globalActions(lines []string) (map[string]any, error) {
	actions := map[string]any{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		var key string
		var value any
		switch fields[0] {
		case "auth-fail-action":
			if len(fields) != 2 || fields[1] != "restricted-vlan" {
				return nil, errors.New("port enablement changes require reapplying a global failure action outside supported RESTCONF ownership")
			}
			key, value = "fail-action", map[string]string{"fail-action": "restricted-vlan"}
		case "auth-timeout-action":
			if len(fields) != 2 || fields[1] != "success" && fields[1] != "failure" && fields[1] != "critical-vlan" {
				return nil, errors.New("port enablement changes require reapplying a global timeout action outside supported RESTCONF ownership")
			}
			key, value = "timeout-action", map[string]bool{fields[1]: true}
		default:
			continue
		}

		if _, exists := actions[key]; exists {
			return nil, errors.New("native authentication contains duplicate global actions")
		}
		actions[key] = value
	}
	return actions, nil
}
