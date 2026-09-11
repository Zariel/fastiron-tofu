package dns

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func validateAddress(address string) error {
	ip, err := netip.ParseAddr(address)
	if err != nil || ip.Zone() != "" || ip.Is4In6() || ip.String() != address || ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("address must be a canonical unicast IPv4 or IPv6 address without a zone")
	}
	return nil
}

func readServers(ctx context.Context, d *fastiron.Device) ([]string, error) {
	if !d.RESTCONFEnabled() {
		return nil, errors.New("DNS configuration currently requires RESTCONF")
	}
	var response struct {
		DNS *struct {
			Servers *struct {
				Server []struct {
					Address string `json:"address"`
					Config  struct {
						Address string `json:"address"`
					} `json:"config"`
				} `json:"server"`
			} `json:"servers"`
		} `json:"openconfig-system:dns"`
	}
	if err := d.DoREST(ctx, http.MethodGet, "/system/dns", nil, &response); err != nil {
		return nil, err
	}
	if response.DNS == nil || response.DNS.Servers == nil {
		return nil, errors.New("RESTCONF DNS response is missing its server container")
	}
	servers := []string{}
	for _, entry := range response.DNS.Servers.Server {
		ip, err := netip.ParseAddr(entry.Address)
		configured, configErr := netip.ParseAddr(entry.Config.Address)
		if err != nil || configErr != nil || ip != configured {
			return nil, errors.New("RESTCONF DNS response contains an inconsistent server identity")
		}
		servers = append(servers, ip.String())
	}
	slices.Sort(servers)
	return servers, nil
}

// applyServer owns one address, preserving server entries outside this resource.
func applyServer(ctx context.Context, d *fastiron.Device, address string, present bool) (bool, error) {
	if err := validateAddress(address); err != nil {
		return false, err
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return false, err
	}
	servers, err := readServers(ctx, d)
	if err != nil {
		return false, err
	}
	exists := slices.Contains(servers, address)
	if exists != present {
		endpoint := path.Join("/system/dns/servers", "server="+url.PathEscape(address))
		method := http.MethodDelete
		var body any
		if present {
			endpoint = "/system/dns/servers"
			method = http.MethodPost
			body = map[string]any{"server": []any{map[string]any{"address": address, "config": map[string]any{"address": address}}}}
		}
		writeErr := d.DoREST(ctx, method, endpoint, body, nil)
		observed, readErr := readServers(ctx, d)
		if readErr != nil {
			return exists, errors.Join(writeErr, readErr)
		}
		exists = slices.Contains(observed, address)
		if exists != present {
			return exists, errors.Join(writeErr, errors.New("DNS server configuration did not converge"))
		}
		for _, neighbor := range servers {
			if neighbor != address && !slices.Contains(observed, neighbor) {
				return exists, errors.New("DNS operation removed an unrelated server")
			}
		}
	}
	return exists, d.Persist(ctx)
}
