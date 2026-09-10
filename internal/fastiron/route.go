package fastiron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

const protocolsPath = "/network-instances/network-instance=default-vrf/protocols"

var staticRoutesPath = path.Join(protocolsPath, "protocol=STATIC,icx-static", "static-routes")

type StaticRoute struct {
	Prefix   netip.Prefix
	NextHop  netip.Addr
	Distance int64
}

func ValidateStaticRoute(v StaticRoute) error {
	if !v.Prefix.IsValid() || v.Prefix != v.Prefix.Masked() || !v.Prefix.Addr().Is4() || v.Prefix.Addr().IsMulticast() {
		return errors.New("prefix must be a canonical IPv4 network in CIDR notation")
	}
	if !v.NextHop.Is4() || v.NextHop.IsUnspecified() || v.NextHop.IsMulticast() {
		return errors.New("next_hop must be a unicast IPv4 address")
	}
	if v.Distance < 1 || v.Distance > 255 {
		return errors.New("distance must be between 1 and 255")
	}
	return nil
}

func (d *Device) StaticRoutes(ctx context.Context) ([]StaticRoute, error) {
	routes, err := d.readStaticRoutes(ctx)
	if errors.Is(err, ErrNotFound) {
		return []StaticRoute{}, nil
	}
	return routes, err
}

func (d *Device) readStaticRoutes(ctx context.Context) ([]StaticRoute, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("static routes currently require RESTCONF")
	}
	var response struct {
		Routes *struct {
			Static []struct {
				Prefix string `json:"prefix"`
				Config *struct {
					Prefix string `json:"prefix"`
				} `json:"config"`
				NextHops *struct {
					NextHop []struct {
						Index  string `json:"index"`
						Config *struct {
							Index   string `json:"index"`
							NextHop string `json:"next-hop"`
							Metric  *int64 `json:"metric"`
						} `json:"config"`
					} `json:"next-hop"`
				} `json:"next-hops"`
			} `json:"static"`
		} `json:"openconfig-network-instance:static-routes"`
	}
	err := d.rest.Do(ctx, http.MethodGet, staticRoutesPath, nil, &response)
	if errors.Is(err, restconf.ErrNotFound) {
		// The static protocol need not exist before its first route. Confirm parent
		// discovery instead of interpreting every missing endpoint as an empty table.
		var parent struct {
			Protocols *struct {
				Protocol []struct {
					Identifier string `json:"identifier"`
					Name       string `json:"name"`
				} `json:"protocol"`
			} `json:"openconfig-network-instance:protocols"`
		}
		if parentErr := d.rest.Do(ctx, http.MethodGet, protocolsPath, nil, &parent); parentErr != nil {
			return nil, parentErr
		}
		if parent.Protocols == nil {
			return nil, errors.New("RESTCONF protocol response is missing its collection")
		}
		for _, protocol := range parent.Protocols.Protocol {
			if protocol.Identifier == "" || protocol.Name == "" {
				return nil, errors.New("RESTCONF protocol response is missing an identity")
			}
			if protocol.Name == "icx-static" {
				return nil, err
			}
		}
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if response.Routes == nil {
		return nil, errors.New("RESTCONF route response is missing its collection")
	}
	routes := []StaticRoute{}
	identities := map[string]bool{}
	for _, entry := range response.Routes.Static {
		prefix, err := netip.ParsePrefix(entry.Prefix)
		if err != nil || entry.Config == nil || entry.Config.Prefix != entry.Prefix || prefix != prefix.Masked() || entry.NextHops == nil || len(entry.NextHops.NextHop) == 0 {
			return nil, errors.New("RESTCONF route response contains an incomplete or inconsistent prefix")
		}
		for _, hop := range entry.NextHops.NextHop {
			gateway, err := netip.ParseAddr(hop.Index)
			if err != nil || hop.Config == nil || hop.Config.Index != hop.Index || hop.Config.NextHop != hop.Index || hop.Config.Metric == nil {
				return nil, errors.New("RESTCONF route response contains an unsupported or inconsistent next hop")
			}
			route := StaticRoute{Prefix: prefix, NextHop: gateway, Distance: *hop.Config.Metric}
			if err := ValidateStaticRoute(route); err != nil {
				return nil, err
			}
			key := entry.Prefix + "|" + hop.Index
			if identities[key] {
				return nil, errors.New("RESTCONF route response contains duplicate next hops")
			}
			identities[key] = true
			routes = append(routes, route)
		}
	}
	slices.SortFunc(routes, func(a, b StaticRoute) int {
		if n := a.Prefix.Addr().Compare(b.Prefix.Addr()); n != 0 {
			return n
		}
		if a.Prefix.Bits() != b.Prefix.Bits() {
			return a.Prefix.Bits() - b.Prefix.Bits()
		}
		return a.NextHop.Compare(b.NextHop)
	})
	return routes, nil
}

func (d *Device) ApplyStaticRoute(ctx context.Context, v StaticRoute, present bool) (*StaticRoute, error) {
	if err := ValidateStaticRoute(v); err != nil {
		return nil, err
	}
	unlock, err := d.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	routes, err := d.readStaticRoutes(ctx)
	createProtocol := errors.Is(err, ErrNotFound)
	if err != nil && !createProtocol {
		return nil, err
	}
	var current *StaticRoute
	for _, route := range routes {
		if route.Prefix == v.Prefix && route.NextHop == v.NextHop {
			current = &route
		}
	}
	if current != nil && current.Distance != v.Distance {
		return current, errors.New("the route has a different distance; refresh and replace its resource")
	}
	if (current != nil) != present {
		endpoint := staticRoutesPath
		method := http.MethodPost
		var body any
		if present {
			hop := map[string]any{"index": v.NextHop.String(), "config": map[string]any{"index": v.NextHop.String(), "next-hop": v.NextHop.String(), "metric": v.Distance}}
			route := map[string]any{"prefix": v.Prefix.String(), "config": map[string]any{"prefix": v.Prefix.String()}, "next-hops": map[string]any{"next-hop": []any{hop}}}
			body = map[string]any{"static": []any{route}}

			// POST creates a prefix; PATCH merges a new next hop into an existing one.
			for _, existing := range routes {
				if existing.Prefix == v.Prefix {
					method = http.MethodPatch
					body = map[string]any{"openconfig-network-instance:static-routes": body}
					break
				}
			}
			if createProtocol {
				endpoint = protocolsPath
				body = map[string]any{"protocol": map[string]any{"identifier": "openconfig-policy-types:STATIC", "name": "icx-static", "config": map[string]any{"identifier": "openconfig-policy-types:STATIC", "name": "icx-static"}, "static-routes": map[string]any{"static": []any{route}}}}
			}
		} else {
			// Deleting the next hop also removes native options absent from this API.
			// Refuse to erase those options until their owner has removed them.
			output, err := d.cli.Run(ctx, true, "show running-config")
			if err != nil {
				return current, err
			}
			if err := routeOptions(output[0], v); err != nil {
				return current, err
			}
			method = http.MethodDelete
			endpoint = path.Join(staticRoutesPath, "static="+url.PathEscape(v.Prefix.String()), "next-hops", "next-hop="+url.PathEscape(v.NextHop.String()))
		}
		writeErr := d.rest.Do(ctx, method, endpoint, body, nil)
		observed, readErr := d.StaticRoutes(ctx)
		if readErr != nil {
			return current, errors.Join(writeErr, readErr)
		}
		current = nil
		for _, route := range observed {
			if route.Prefix == v.Prefix && route.NextHop == v.NextHop {
				current = &route
			}
		}
		if (current != nil) != present || (current != nil && *current != v) {
			return current, errors.Join(writeErr, errors.New("static route did not converge"))
		}
		for _, neighbor := range routes {
			if neighbor.Prefix == v.Prefix && neighbor.NextHop == v.NextHop {
				continue
			}
			if !slices.Contains(observed, neighbor) {
				return current, errors.New("static route operation changed an unrelated next hop")
			}
		}
	}
	if d.config.Persistence == "after_each_write" {
		return current, d.save(ctx)
	}
	return current, nil
}

func routeOptions(config string, v StaticRoute) error {
	if _, err := configuration(config); err != nil {
		return err
	}
	expected := fmt.Sprintf("ip route %s %s", v.Prefix, v.NextHop)
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if line == expected || line == fmt.Sprintf("%s distance %d", expected, v.Distance) {
			return nil
		}
		if strings.HasPrefix(line, expected+" ") {
			return errors.New("route has additional native options; remove them before destroying the route")
		}
	}
	return errors.New("static route was not found in native configuration")
}
