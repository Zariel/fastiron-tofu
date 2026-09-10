package provider

import (
	"encoding/json"
	"net/http"
)

func (s *testSwitch) lldpREST(w http.ResponseWriter, r *http.Request) {
	switch r.URL.EscapedPath() {
	case "/restconf/data/lldp/config":
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"openconfig-lldp:config": map[string]any{"enabled": s.lldp}})
			return
		}
		if r.Method == "PATCH" {
			var body struct {
				Config struct {
					Enabled *bool `json:"enabled"`
				} `json:"config"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Config.Enabled == nil {
				w.WriteHeader(400)
				return
			}
			s.lldp = *body.Config.Enabled
			s.writes++
			w.WriteHeader(204)
			return
		}
	case "/restconf/data/lldp/interfaces/interface=ethernet%201%2F1%2F2":
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"openconfig-lldp:interface": []any{map[string]any{"name": "ethernet 1/1/2", "config": map[string]any{"name": "ethernet 1/1/2", "enabled": s.lldpPort}}}})
			return
		}
	case "/restconf/data/lldp/interfaces":
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"openconfig-lldp:interfaces": map[string]any{"interface": []any{map[string]any{"name": "ethernet 1/1/2", "config": map[string]any{"name": "ethernet 1/1/2", "enabled": s.lldpPort}}, map[string]any{"name": "ethernet 1/1/3", "config": map[string]any{"name": "ethernet 1/1/3", "enabled": true}}}}})
			return
		}
		if r.Method == "PATCH" {
			var body struct {
				Interfaces struct {
					Interface []struct {
						Name   string `json:"name"`
						Config struct {
							Name    string `json:"name"`
							Enabled *bool  `json:"enabled"`
						} `json:"config"`
					} `json:"interface"`
				} `json:"interfaces"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Interfaces.Interface) != 1 {
				w.WriteHeader(400)
				return
			}
			entry := body.Interfaces.Interface[0]
			if entry.Name != "ethernet 1/1/2" || entry.Config.Name != entry.Name || entry.Config.Enabled == nil {
				w.WriteHeader(400)
				return
			}
			s.lldpPort = *entry.Config.Enabled
			s.writes++
			w.WriteHeader(204)
			return
		}
	}
	w.WriteHeader(404)
}
