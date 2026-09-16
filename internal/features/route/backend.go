package route

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"slices"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

const protocolsPath = "/network-instances/network-instance=default-vrf/protocols"

var staticRoutesPath = path.Join(protocolsPath, "protocol=STATIC,icx-static", "static-routes")

type route struct {
	Prefix   netip.Prefix
	NextHop  netip.Addr
	Distance int64
}

func validateRoute(v route) error {
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

func readRoutes(ctx context.Context, d *fastiron.Device) ([]route, error) {
	// Missing or stale REST objects do not establish native route absence.
	if _, err := cachedRoutes(ctx, d); err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		return nil, err
	}
	return nativeRoutes(ctx, d)
}

func cachedRoutes(ctx context.Context, d *fastiron.Device) ([]route, error) {
	if !d.RESTCONFEnabled() {
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
	err := d.ReadREST(ctx, staticRoutesPath, &response)
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
		if parentErr := d.ReadREST(ctx, protocolsPath, &parent); parentErr != nil {
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
		return nil, fastiron.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if response.Routes == nil {
		return nil, errors.New("RESTCONF route response is missing its collection")
	}
	routes := []route{}
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
			route := route{Prefix: prefix, NextHop: gateway, Distance: *hop.Config.Metric}
			if err := validateRoute(route); err != nil {
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
	return routes, nil
}

func nativeRoutes(ctx context.Context, d *fastiron.Device) ([]route, error) {
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	configured, err := document.IPv4Routes()
	if err != nil {
		return nil, err
	}
	routes := make([]route, 0, len(configured))
	for _, current := range configured {
		routes = append(routes, route{Prefix: current.Prefix, NextHop: current.NextHop, Distance: current.Distance})
	}
	slices.SortFunc(routes, func(a, b route) int {
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

func applyRoute(ctx context.Context, d *fastiron.Device, v route, present bool) (*route, error) {
	if err := validateRoute(v); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*route, error) {
		// Cached containers select the REST request shape; native configuration
		// determines whether a mutation is needed and whether it took effect.
		cached, err := cachedRoutes(ctx, d)
		createProtocol := errors.Is(err, fastiron.ErrNotFound)
		if err != nil && !createProtocol {
			return nil, err
		}
		routes, err := nativeRoutes(ctx, d)
		if err != nil {
			return nil, err
		}
		var current *route
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
				for _, existing := range cached {
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
				document, err := d.RunningConfig(ctx)
				if err != nil {
					return current, err
				}
				if err := routeOptions(document, v); err != nil {
					return current, err
				}
				method = http.MethodDelete
				endpoint = path.Join(staticRoutesPath, "static="+url.PathEscape(v.Prefix.String()), "next-hops", "next-hop="+url.PathEscape(v.NextHop.String()))
			}
			writeErr := update.REST(method, endpoint, body)
			observed, readErr := readRoutes(ctx, d)
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
		return current, nil
	})
}

func routeOptions(document *nativeconfig.Document, v route) error {
	routes, err := document.IPv4Routes()
	if err != nil {
		return err
	}
	for _, current := range routes {
		if current.Prefix != v.Prefix || current.NextHop != v.NextHop {
			continue
		}
		if current.HasOptions {
			return errors.New("route has additional native options; remove them before destroying the route")
		}
		if current.Distance != v.Distance {
			return errors.New("native route distance changed; refresh before destroying the route")
		}
		return nil
	}
	return errors.New("static route was not found in native configuration")
}
