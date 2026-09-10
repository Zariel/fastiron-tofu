package provider

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *testSwitch) addressREST(w http.ResponseWriter, r *http.Request) {
	if s.ve == nil {
		w.WriteHeader(404)
		return
	}
	path := r.URL.EscapedPath()
	ipv6 := strings.Contains(path, "/ipv6/")
	base := "/restconf/data/interfaces/interface=ve%2053/routed-vlan/ipv4/addresses"
	if ipv6 {
		base = "/restconf/data/interfaces/interface=ve%2053/routed-vlan/ipv6/addresses"
	}
	if r.Method == "GET" && path == base {
		entries := []any{}
		for ip, bits := range s.addresses {
			if strings.Contains(ip, ":") != ipv6 {
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
		if entry.IP != entry.Config.IP || strings.Contains(entry.IP, ":") != ipv6 {
			w.WriteHeader(400)
			return
		}
		s.addresses[entry.IP] = entry.Config.Bits
		s.writes++
		w.WriteHeader(201)
		return
	}
	if r.Method == "DELETE" && strings.HasPrefix(path, base+"/address=") {
		ip := strings.TrimPrefix(path, base+"/address=")
		delete(s.addresses, ip)
		s.writes++
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(404)
}
