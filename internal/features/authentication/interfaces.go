package authentication

import (
	"context"
	"errors"
	"path"
	"strconv"
	"strings"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"

	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type interfaceConfig struct {
	Dot1XEnabled bool
	MACEnabled   bool
	PortControl  string
}

// readInterfaces uses native configuration because RESTCONF can retain
// a port in both its previous and current control-mode lists after a transition.
func readInterfaces(ctx context.Context, d *fastiron.Device) (map[string]interfaceConfig, error) {
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	interfaces, _, err := nativeAuthenticationInterfaces(document)
	return interfaces, err
}

func nativeAuthenticationInterfaces(document *nativeconfig.Document) (map[string]interfaceConfig, []string, error) {
	var unowned []string
	interfaces := map[string]interfaceConfig{}
	controls := map[string]string{}
	active := false
	for _, command := range document.Commands {
		line := command.Text
		f := command.Fields
		if len(f) == 0 {
			continue
		}
		if len(f) == 1 && f[0] == "authentication" && command.Parent == -1 {
			active = true
			unowned = append(unowned, "authentication")
			continue
		}
		if command.Parent == -1 {
			active = false
		}

		kind, mode := "", ""
		var ports []string
		switch {
		case (active || command.Parent == -1) && len(f) >= 3 && f[0] == "dot1x" && f[1] == "port-control":
			kind, mode = "control", f[2]
			if mode != "auto" && mode != "force-authorized" && mode != "force-unauthorized" {
				return nil, nil, errors.New("unsupported native authentication port-control mode")
			}
			ports = f[3:]
		case active && len(f) > 2 && f[0] == "dot1x" && f[1] == "enable":
			kind, ports = "dot1x", f[2:]
		case active && len(f) > 2 && f[0] == "mac-authentication" && f[1] == "enable":
			kind, ports = "mac", f[2:]
		default:
			unowned = append(unowned, line)
			continue
		}
		names, err := authenticationPorts(ports)
		if err != nil {
			return nil, nil, err
		}
		for _, name := range names {
			current, ok := interfaces[name]
			if !ok {
				current.PortControl = "force-authorized"
			}
			switch kind {
			case "dot1x":
				current.Dot1XEnabled = true
			case "mac":
				current.MACEnabled = true
			case "control":
				if previous, ok := controls[name]; ok && previous != mode {
					return nil, nil, errors.New("native authentication contains conflicting port-control modes")
				}
				controls[name] = mode
				current.PortControl = mode
			}
			interfaces[name] = current
		}
	}
	return interfaces, unowned, nil
}

func authenticationPorts(fields []string) ([]string, error) {
	if len(fields) < 2 || fields[0] != "ethe" && fields[0] != "ethernet" {
		return nil, errors.New("unsupported native authentication port expression")
	}
	var names []string
	for i := 1; i < len(fields); i++ {
		if fields[i] == "ethe" || fields[i] == "ethernet" {
			if i+1 == len(fields) {
				return nil, errors.New("incomplete native authentication port expression")
			}
			continue
		}
		if !interfaceid.EthernetPort(fields[i]) {
			return nil, errors.New("invalid native authentication interface")
		}
		parts := strings.Split(fields[i], "/")
		first, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, errors.New("invalid native authentication port number")
		}
		last := first
		if i+1 < len(fields) && fields[i+1] == "to" {
			if i+2 >= len(fields) || !interfaceid.EthernetPort(fields[i+2]) {
				return nil, errors.New("incomplete native authentication port range")
			}
			end := strings.Split(fields[i+2], "/")
			if parts[0] != end[0] || parts[1] != end[1] {
				return nil, errors.New("native authentication port range crosses slots")
			}
			last, err = strconv.Atoi(end[2])
			// Bound expansion of device-supplied ranges to limit memory usage.
			if err != nil || last < first || last-first > 65535 {
				return nil, errors.New("invalid or excessive native authentication port range")
			}
			i += 2
		}
		for offset := 0; offset <= last-first; offset++ {
			names = append(names, "ethernet "+path.Join(parts[0], parts[1], strconv.Itoa(first+offset)))
		}
	}
	return names, nil
}
