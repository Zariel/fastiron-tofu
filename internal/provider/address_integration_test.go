package provider

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
)

func (s *testSwitch) addressREST(w http.ResponseWriter, r *http.Request) {
	management := strings.HasPrefix(r.URL.EscapedPath(), "/restconf/data/interfaces/interface=management%201/")
	if (!management && s.ve == nil) || (management && s.managementAddresses == nil) {
		w.WriteHeader(404)
		return
	}
	path := r.URL.EscapedPath()
	ipv6 := strings.Contains(path, "/ipv6/")
	base := "/restconf/data/interfaces/interface=ve%2053/routed-vlan/ipv4/addresses"
	if ipv6 {
		base = "/restconf/data/interfaces/interface=ve%2053/routed-vlan/ipv6/addresses"
	}
	addresses := s.addresses
	if management {
		addresses = s.managementAddresses
		base = strings.Replace(base, "interface=ve%2053/routed-vlan", "interface=management%201/subinterfaces/subinterface=0", 1)
	}
	if r.Method == "GET" && path == base {
		entries := []any{}
		for ip, bits := range addresses {
			if netip.MustParseAddr(ip).Is6() != ipv6 {
				continue
			}
			entry := map[string]any{"ip": ip, "config": map[string]any{"ip": ip, "prefix-length": bits}, "vrrp": map[string]any{}}
			if s.addressChild {
				entry["vrrp"] = map[string]any{"vrrp-group": []any{map[string]any{"virtual-router-id": 1}}}
			}
			entries = append(entries, entry)
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-if-ip:addresses": map[string]any{"address": entries}})
		return
	}
	if r.Method == "POST" && path == base {
		var body struct {
			Address []struct {
				IP     string `json:"ip"`
				Config struct {
					IP   string `json:"ip"`
					Bits int    `json:"prefix-length"`
				} `json:"config"`
			} `json:"address"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Address) != 1 {
			w.WriteHeader(400)
			return
		}
		entry := body.Address[0]
		address, err := netip.ParseAddr(entry.IP)
		if err != nil || entry.IP != entry.Config.IP || address.Is6() != ipv6 {
			w.WriteHeader(400)
			return
		}
		addresses[entry.IP] = entry.Config.Bits
		s.writes++
		w.WriteHeader(201)
		return
	}
	if r.Method == "DELETE" && strings.HasPrefix(path, base+"/address=") {
		ip := strings.TrimPrefix(path, base+"/address=")
		delete(addresses, ip)
		s.writes++
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(404)
}
