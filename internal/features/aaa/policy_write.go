package aaa

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
	"strings"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func validatePolicy(p policy) error {
	if len(p.LoginMethods) < 1 || len(p.LoginMethods) > 3 {
		return errors.New("login_methods must contain one to three distinct methods")
	}
	seen := map[string]bool{}
	for _, method := range p.LoginMethods {
		if !slices.Contains([]string{"local", "radius", "tacacs+"}, method) || seen[method] {
			return errors.New("login_methods must be distinct local, radius or tacacs+ methods in attempt order")
		}
		seen[method] = true
	}
	if p.Dot1XDefault != "" && p.Dot1XDefault != "none" && p.Dot1XDefault != "radius" {
		return errors.New("dot1x_default must be unset, none or radius")
	}
	seen = map[string]bool{}
	for _, action := range p.CoAIgnore {
		if !slices.Contains([]string{"disable-port", "dm-request", "flip-port", "modify-acl", "reauth-host"}, action) || seen[action] {
			return errors.New("coa_ignore must contain distinct supported CoA actions")
		}
		seen[action] = true
	}
	return nil
}

func nativeAAAPolicy(output string) (*policy, []string, error) {
	document, err := nativeconfig.Parse(output)
	if err != nil {
		return nil, nil, err
	}
	p := &policy{}
	var neighbors []string
	seen := map[string]bool{}
	for _, command := range document.Commands {
		line := command.Text
		if command.Parent != -1 {
			neighbors = append(neighbors, line)
			continue
		}
		f := command.Fields
		key := ""
		switch {
		case strings.HasPrefix(line, "aaa authentication login "):
			key = "login"
			if len(f) < 5 || f[3] != "default" {
				return nil, nil, errors.New("native login policy has settings outside RESTCONF ownership")
			}
			p.LoginMethods = slices.Clone(f[4:])
		case strings.HasPrefix(line, "aaa authentication dot1x "):
			key = "dot1x"
			if len(f) != 5 || f[3] != "default" {
				return nil, nil, errors.New("native dot1x policy has settings outside RESTCONF ownership")
			}
			p.Dot1XDefault = f[4]
		case strings.HasPrefix(line, "aaa authorization coa "):
			if len(f) == 4 && f[3] == "enable" {
				key = "coa-enable"
				p.CoAEnabled = true
			} else if len(f) >= 5 && f[3] == "ignore" {
				p.CoAIgnore = append(p.CoAIgnore, f[4:]...)
			} else {
				return nil, nil, errors.New("native CoA policy has settings outside RESTCONF ownership")
			}
		default:
			neighbors = append(neighbors, line)
		}
		if key != "" {
			if seen[key] {
				return nil, nil, errors.New("duplicate native AAA policy configuration")
			}
			seen[key] = true
		}
	}
	if err := validatePolicy(*p); err != nil {
		return nil, nil, errors.New("native AAA policy has unsupported or incomplete settings")
	}
	slices.Sort(p.CoAIgnore)
	return p, neighbors, nil
}

func sameAAAPolicy(a, b policy) bool {
	return slices.Equal(a.LoginMethods, b.LoginMethods) && a.Dot1XDefault == b.Dot1XDefault && a.CoAEnabled == b.CoAEnabled && slices.Equal(a.CoAIgnore, b.CoAIgnore)
}

// readConfiguration distinguishes an absent native dot1x policy from explicit
// none authentication, which RESTCONF reports identically.
func readConfiguration(ctx context.Context, d *fastiron.Device) (*policy, error) {
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	p, _, err := configuration(ctx, d)
	return p, err
}

func configuration(ctx context.Context, d *fastiron.Device) (*policy, []string, error) {
	projected, err := readPolicy(ctx, d)
	if err != nil {
		return nil, nil, err
	}
	output, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	native, neighbors, err := nativeAAAPolicy(output)
	if err != nil {
		return nil, nil, err
	}
	comparable := *native
	if comparable.Dot1XDefault == "" && projected.Dot1XDefault == "none" {
		comparable.Dot1XDefault = "none"
	}
	if !sameAAAPolicy(comparable, *projected) {
		return native, nil, errors.New("native and RESTCONF AAA policy disagree; retry after synchronization")
	}
	return native, neighbors, nil
}

func applyPolicy(ctx context.Context, d *fastiron.Device, desired policy) (*policy, error) {
	if err := d.CheckAAAChanges(); err != nil {
		return nil, err
	}
	if err := validatePolicy(desired); err != nil {
		return nil, err
	}
	desired.CoAIgnore = slices.Clone(desired.CoAIgnore)
	slices.Sort(desired.CoAIgnore)

	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*policy, error) {
		current, neighbors, err := configuration(ctx, d)
		if err != nil {
			return current, err
		}

		// Verify each native command family before moving to the next. In particular,
		// CoA's parent PATCH can update the REST projection without applying ignores.
		write := func(method, endpoint string, body any, expected policy) error {
			writeErr := update.REST(method, endpoint, body)
			observed, after, readErr := configuration(ctx, d)
			if observed != nil {
				current = observed
			}
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			if !slices.Equal(neighbors, after) {
				return errors.Join(writeErr, errors.New("AAA policy operation changed unrelated native configuration"))
			}
			if !sameAAAPolicy(*current, expected) {
				return errors.Join(writeErr, errors.New("AAA policy did not converge at "+endpoint))
			}
			return writeErr
		}
		root := "/system/aaa"
		if !slices.Equal(current.CoAIgnore, desired.CoAIgnore) {
			flags := map[string]bool{}
			for _, action := range []string{"disable-port", "dm-request", "flip-port", "modify-acl", "reauth-host"} {
				flags[action] = slices.Contains(desired.CoAIgnore, action)
			}
			expected := *current
			expected.CoAIgnore = desired.CoAIgnore
			if err := write(http.MethodPatch, path.Join(root, "authorization/coa/ignore"), map[string]any{"ignore": flags}, expected); err != nil {
				return current, err
			}
		}
		if current.CoAEnabled != desired.CoAEnabled {
			expected := *current
			expected.CoAEnabled = desired.CoAEnabled
			if err := write(http.MethodPatch, path.Join(root, "authorization/coa"), map[string]any{"coa": map[string]bool{"enable": desired.CoAEnabled}}, expected); err != nil {
				return current, err
			}
		}
		if current.Dot1XDefault != desired.Dot1XDefault {
			expected := *current
			expected.Dot1XDefault = desired.Dot1XDefault
			method, endpoint := http.MethodDelete, path.Join(root, "authentication/dot1x")
			var body any
			if desired.Dot1XDefault != "" {
				method, endpoint = http.MethodPatch, path.Join(root, "authentication")
				body = map[string]any{"authentication": map[string]any{"icx-openconfig-aaa-aug:dot1x": map[string]string{"default": desired.Dot1XDefault}}}
			}
			if current.Dot1XDefault == "" && desired.Dot1XDefault == "none" {
				// An implicit REST default suppresses explicit native none creation.
				// Native absence is already verified; clear only its projection and create
				// immediately, without an intervening read or another authentication mode.
				err := update.DeleteIfPresent(path.Join(root, "authentication/dot1x"))
				if err != nil {
					return current, err
				}
				method = http.MethodPost
				body = map[string]any{"icx-openconfig-aaa-aug:dot1x": map[string]string{"default": "none"}}
			}
			if err := write(method, endpoint, body, expected); err != nil {
				return current, err
			}
		}
		// Login policy changes run last because they can change transport access.
		if !slices.Equal(current.LoginMethods, desired.LoginMethods) {
			if err := write(http.MethodPut, path.Join(root, "authentication/login"), map[string]any{"login": map[string]any{"default": desired.LoginMethods}}, desired); err != nil {
				return current, err
			}
		}
		return current, nil
	})
}
