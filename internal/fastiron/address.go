package fastiron

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

type (
	InterfaceAddress struct {
		Interface string
		Address   netip.Prefix
	}
	addressState struct {
		Prefix      netip.Prefix
		HasChildren bool
	}
)

func ValidateAddressInterface(name string) error {
	id, err := strconv.ParseInt(strings.TrimPrefix(name, "ve "), 10, 64)
	if err != nil || id < 1 || id > 4094 || name != "ve "+strconv.FormatInt(id, 10) {
		return errors.New("address resources currently require a canonical VE interface name: ve <id>")
	}
	return nil
}

func ValidateInterfaceAddress(v InterfaceAddress) error {
	if err := ValidateAddressInterface(v.Interface); err != nil {
		return err
	}
	if !v.Address.IsValid() || v.Address.Addr().Is4In6() || v.Address.Addr().IsUnspecified() || v.Address.Addr().IsMulticast() {
		return errors.New("address must be a unicast IPv4 or IPv6 CIDR")
	}
	return nil
}

func addressPath(name string, ipv6 bool) string {
	family := "ipv4"
	if ipv6 {
		family = "ipv6"
	}
	return "/interfaces/interface=" + url.PathEscape(name) + "/routed-vlan/" + family + "/addresses"
}

func (d *Device) readAddresses(ctx context.Context, name string, ipv6 bool) (map[netip.Addr]addressState, error) {
	if err := ValidateAddressInterface(name); err != nil {
		return nil, err
	}
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("interface addressing currently requires RESTCONF")
	}
	var response struct {
		Addresses *struct {
			Address []struct {
				IP     string `json:"ip"`
				Config *struct {
					IP           string `json:"ip"`
					PrefixLength *int   `json:"prefix-length"`
				} `json:"config"`
				VRRP map[string]json.RawMessage `json:"vrrp"`
			} `json:"address"`
		} `json:"openconfig-if-ip:addresses"`
	}
	err := d.rest.Do(ctx, http.MethodGet, addressPath(name, ipv6), nil, &response)
	if errors.Is(err, restconf.ErrNotFound) {
		id, _ := strconv.ParseInt(strings.TrimPrefix(name, "ve "), 10, 64)
		// A missing address endpoint is not absence unless the parent is absent too.
		if _, parentErr := d.VE(ctx, id); errors.Is(parentErr, ErrNotFound) {
			return nil, ErrNotFound
		}
	}
	if err != nil {
		return nil, err
	}
	if response.Addresses == nil {
		return nil, errors.New("RESTCONF address response is missing its collection")
	}
	addresses := map[netip.Addr]addressState{}
	for _, entry := range response.Addresses.Address {
		ip, err := netip.ParseAddr(entry.IP)
		if err != nil || ip.Is4In6() || ip.Is6() != ipv6 || entry.Config == nil || entry.Config.PrefixLength == nil {
			return nil, errors.New("RESTCONF address response is missing an identity or prefix")
		}
		configured, err := netip.ParseAddr(entry.Config.IP)
		prefix := netip.PrefixFrom(ip, *entry.Config.PrefixLength)
		if err != nil || configured != ip || !prefix.IsValid() {
			return nil, errors.New("RESTCONF address response contains an inconsistent identity")
		}
		if _, duplicate := addresses[ip]; duplicate {
			return nil, errors.New("RESTCONF address response contains duplicate IP identities")
		}
		addresses[ip] = addressState{Prefix: prefix, HasChildren: len(entry.VRRP) > 0}
	}
	return addresses, nil
}

func (d *Device) InterfaceAddresses(ctx context.Context, name string, ipv6 bool) ([]netip.Prefix, error) {
	addresses, err := d.readAddresses(ctx, name, ipv6)
	if err != nil {
		return nil, err
	}
	prefixes := make([]netip.Prefix, 0, len(addresses))
	for _, entry := range addresses {
		prefixes = append(prefixes, entry.Prefix)
	}
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int { return a.Addr().Compare(b.Addr()) })
	return prefixes, nil
}

func (d *Device) ApplyInterfaceAddress(ctx context.Context, v InterfaceAddress, present bool) (*netip.Prefix, error) {
	if err := ValidateInterfaceAddress(v); err != nil {
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
	addresses, err := d.readAddresses(ctx, v.Interface, v.Address.Addr().Is6())
	if errors.Is(err, ErrNotFound) && !present {
		if d.config.Persistence == "after_each_write" {
			return nil, d.save(ctx)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry, exists := addresses[v.Address.Addr()]
	if exists && entry.Prefix != v.Address {
		return &entry.Prefix, errors.New("the IP address has a different prefix; refresh and replace its address resource")
	}
	if exists != present {
		if !present && entry.HasChildren {
			return &entry.Prefix, errors.New("address has VRRP child configuration; remove it before destroying the address")
		}
		path := addressPath(v.Interface, v.Address.Addr().Is6())
		method := http.MethodPost
		var body any = map[string]any{"address": []any{map[string]any{"ip": v.Address.Addr().String(), "config": map[string]any{"ip": v.Address.Addr().String(), "prefix-length": v.Address.Bits()}}}}
		if !present {
			method = http.MethodDelete
			path += "/address=" + url.PathEscape(v.Address.Addr().String())
			body = nil
		}
		writeErr := d.rest.Do(ctx, method, path, body, nil)
		observed, readErr := d.readAddresses(ctx, v.Interface, v.Address.Addr().Is6())
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		actual, found := observed[v.Address.Addr()]
		var current *netip.Prefix
		if found {
			current = &actual.Prefix
		}
		if found != present || (found && actual.Prefix != v.Address) {
			return current, errors.Join(writeErr, errors.New("interface address did not converge"))
		}
		// Keyed deletion must preserve neighboring addresses, including their masks.
		for ip, neighbor := range addresses {
			if ip != v.Address.Addr() && observed[ip].Prefix != neighbor.Prefix {
				return current, errors.New("address operation changed an unrelated address")
			}
		}
		entry, exists = actual, found
	}
	var current *netip.Prefix
	if exists {
		current = &entry.Prefix
	}
	if d.config.Persistence == "after_each_write" {
		return current, d.save(ctx)
	}
	return current, nil
}
