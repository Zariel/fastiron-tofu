package aaa

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func validateServer(s server) error {
	if s.Kind != "radius" && s.Kind != "tacacs" {
		return errors.New("AAA server kind must be radius or tacacs")
	}
	if ip, err := netip.ParseAddr(s.Address); err == nil {
		if ip.String() != s.Address || ip.Zone() != "" || ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() {
			return errors.New("AAA server address must be canonical unicast without a zone")
		}
	} else {
		if s.Address == "" || len(s.Address) > 253 {
			return errors.New("AAA server address must be an IP address or DNS hostname")
		}
		for _, label := range strings.Split(s.Address, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return errors.New("invalid AAA server hostname")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return errors.New("AAA server hostname must use lowercase DNS labels")
				}
			}
		}
	}
	if s.AuthPort < 1 || s.AuthPort > 65535 || s.Kind == "radius" && (s.AcctPort < 1 || s.AcctPort > 65535) || s.Kind == "tacacs" && s.AcctPort != 0 {
		return errors.New("invalid AAA server protocol ports")
	}
	if s.Purpose != "default" && s.Purpose != "accounting-only" && s.Purpose != "authentication-only" && !(s.Kind == "tacacs" && s.Purpose == "authorization-only") {
		return errors.New("invalid AAA server purpose")
	}
	return nil
}

type nativeAAAServer struct {
	server   server
	hasKey   bool
	position int
}

// nativeAAA rejects settings the RESTCONF server payload cannot preserve. Secret
// values stay local; errors never include the source configuration line.
func nativeAAA(output string, desired server) (*nativeAAAServer, []string, error) {
	document, err := nativeconfig.Parse(output)
	if err != nil {
		return nil, nil, err
	}
	var current *nativeAAAServer
	var neighbors []string
	for _, command := range document.Commands {
		line := command.Text
		if command.Parent != -1 {
			continue
		}
		line = strings.TrimSpace(line)
		fields := command.Fields
		if len(fields) > 0 && fields[0] == "no" {
			fields = fields[1:]
			if len(fields) > 0 && (fields[0] == "radius-server" || fields[0] == "tacacs-server" || fields[0] == "aaa") {
				neighbors = append(neighbors, line)
			}
			continue
		}
		if len(fields) == 0 || !(fields[0] == "radius-server" || fields[0] == "tacacs-server" || fields[0] == "aaa") {
			continue
		}
		if len(fields) < 3 || fields[0] != desired.Kind+"-server" || fields[1] != "host" || fields[2] != desired.Address {
			neighbors = append(neighbors, line)
			continue
		}
		if current != nil {
			return nil, nil, errors.New("duplicate native AAA server identity")
		}
		s := server{Kind: desired.Kind, Address: desired.Address, AuthPort: 49, Purpose: "default"}
		if s.Kind == "radius" {
			s.AuthPort, s.AcctPort = 1812, 1813
		}
		current = &nativeAAAServer{position: len(neighbors)}
		seen := map[string]bool{}
		for i := 3; i < len(fields); i++ {
			token := fields[i]
			if seen[token] {
				return nil, nil, errors.New("duplicate native AAA server option")
			}
			seen[token] = true
			switch token {
			case "auth-port", "acct-port":
				if i+1 == len(fields) || token == "acct-port" && s.Kind != "radius" {
					return nil, nil, errors.New("invalid native AAA server port")
				}
				i++
				port, err := strconv.ParseInt(fields[i], 10, 64)
				if err != nil {
					return nil, nil, errors.New("invalid native AAA server port")
				}
				if token == "auth-port" {
					s.AuthPort = port
				} else {
					s.AcctPort = port
				}
			case "default", "authentication-only", "accounting-only", "authorization-only":
				if seen["purpose"] {
					return nil, nil, errors.New("duplicate native AAA server purpose")
				}
				seen["purpose"] = true
				s.Purpose = token
			case "key":
				remaining := fields[i+1:]
				if len(remaining) != 1 && !(len(remaining) == 2 && (remaining[0] == "0" || remaining[0] == "1" || remaining[0] == "2")) {
					return nil, nil, errors.New("native AAA server has unsupported key syntax or trailing options")
				}
				current.hasKey = true
				i = len(fields)
			default:
				return nil, nil, errors.New("native AAA server has settings not supported by RESTCONF ownership")
			}
		}
		if err := validateServer(s); err != nil {
			return nil, nil, errors.New("native AAA server has invalid configuration")
		}
		current.server = s
	}
	return current, neighbors, nil
}

// applyServer owns one server. TACACS currently requires a per-server key.
// A nil RADIUS secret requires a server without a per-server key; removing an
// existing RADIUS key must be expressed as an explicit server replacement.
func applyServer(ctx context.Context, d *fastiron.Device, desired server, secret *string, present bool) (*server, error) {
	if err := d.CheckAAAChanges(); err != nil {
		return nil, err
	}
	if err := validateServer(desired); err != nil {
		return nil, err
	}
	// Omitting a TACACS key can still create a native key clause. Until a
	// keyless operation is supported, reject it before changing the switch.
	if present && desired.Kind == "tacacs" && secret == nil {
		return nil, errors.New("TACACS writes currently require a per-server key; keyless configuration is not implemented")
	}
	if secret != nil {
		limit := 64
		if desired.Kind == "tacacs" {
			limit = 32
		}
		if len(*secret) == 0 || len(*secret) > limit || strings.IndexFunc(*secret, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return nil, errors.New("AAA key must be nonempty, contain no whitespace, and fit the protocol length limit")
		}
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err = d.Discover(ctx); err != nil {
		return nil, err
	}
	servers, err := readServers(ctx, d)
	if err != nil {
		return nil, err
	}
	output, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	native, neighbors, err := nativeAAA(output, desired)
	if err != nil {
		return nil, err
	}
	var current *server
	for _, s := range servers {
		if s.Kind == desired.Kind && s.Address == desired.Address {
			current = &s
		}
	}
	if (current == nil) != (native == nil) || current != nil && *current != native.server {
		return current, errors.New("native and RESTCONF AAA server configuration disagree; retry after synchronization")
	}
	if present && native != nil && native.hasKey && secret == nil {
		return current, errors.New("existing AAA server has a key; supply its configured key or explicitly replace the server to remove it")
	}
	before := native
	if present || current != nil {
		endpoint := "/system/aaa/server-groups"
		method := http.MethodPatch
		var body any
		group := desired.Kind + "-default-group"
		if present {
			config := map[string]any{"icx-openconfig-aaa-aug:purpose": desired.Purpose}
			if desired.Kind == "radius" {
				config["auth-port"], config["acct-port"] = desired.AuthPort, desired.AcctPort
			} else {
				config["port"] = desired.AuthPort
			}
			if secret != nil {
				config["secret-key"] = *secret
			}
			// FastIron can reset omitted fields, so send complete owned metadata and
			// configured key together. Never replay opaque keys returned by the API.
			server := map[string]any{
				"address":    desired.Address,
				"config":     map[string]any{"name": desired.Address, "address": desired.Address},
				desired.Kind: map[string]any{"config": config},
			}
			body = map[string]any{"server-groups": map[string]any{"server-group": []any{map[string]any{
				"name":    group,
				"config":  map[string]any{"name": group, "type": strings.ToUpper(desired.Kind)},
				"servers": map[string]any{"server": []any{server}},
			}}}}
		} else {
			method = http.MethodDelete
			endpoint = path.Join(endpoint, "server-group="+group, "servers", "server="+url.PathEscape(desired.Address))
		}
		writeErr := d.DoREST(ctx, method, endpoint, body, nil)
		observed, readErr := readServers(ctx, d)
		if readErr != nil {
			return current, errors.Join(writeErr, readErr)
		}
		current = nil
		for _, s := range observed {
			if s.Kind == desired.Kind && s.Address == desired.Address {
				current = &s
			}
		}
		output, nativeErr := d.RunningConfig(ctx)
		if nativeErr != nil {
			return current, errors.Join(writeErr, nativeErr)
		}
		native, after, parseErr := nativeAAA(output, desired)
		if parseErr != nil {
			return current, errors.Join(writeErr, parseErr)
		}
		// Existing server order determines authentication priority. A metadata
		// update must not silently move the server behind its neighbors.
		if present && before != nil && native != nil && before.position != native.position {
			return current, errors.Join(writeErr, errors.New("AAA update changed native server ordering"))
		}
		if !slices.Equal(neighbors, after) {
			return current, errors.Join(writeErr, errors.New("AAA operation changed unrelated native configuration"))
		}
		if present && (current == nil || *current != desired || native == nil || native.server != desired || native.hasKey != (secret != nil)) || !present && (current != nil || native != nil) {
			return current, errors.Join(writeErr, errors.New("AAA server configuration did not converge"))
		}
		// Metadata read-back cannot prove a secret write succeeded after an ambiguous
		// transport failure. Leave the failure visible so reconciliation retries it.
		if writeErr != nil {
			return current, writeErr
		}
	}
	return current, d.Persist(ctx)
}
