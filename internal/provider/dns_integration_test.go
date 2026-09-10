package provider

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *testSwitch) dnsREST(w http.ResponseWriter, r *http.Request) {
	const base = "/restconf/data/system/dns"
	if r.Method == "GET" && r.URL.Path == base {
		entries := []any{}
		for address := range s.dns {
			entries = append(entries, map[string]any{"address": address, "config": map[string]any{"address": address}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-system:dns": map[string]any{"servers": map[string]any{"server": entries}}})
		return
	}
	if r.Method == "POST" && r.URL.Path == base+"/servers" {
		var body struct {
			Servers []struct {
				Address string `json:"address"`
				Config  struct {
					Address string `json:"address"`
				} `json:"config"`
			} `json:"server"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Servers) != 1 || body.Servers[0].Address != body.Servers[0].Config.Address {
			w.WriteHeader(400)
			return
		}
		s.dns[body.Servers[0].Address] = true
		s.writes++
		w.WriteHeader(201)
		return
	}
	if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, base+"/servers/server=") {
		delete(s.dns, strings.TrimPrefix(r.URL.Path, base+"/servers/server="))
		s.writes++
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(404)
}
