package provider

import (
	"encoding/json"
	"net/http"
)

func (s *testSwitch) poeREST(w http.ResponseWriter, r *http.Request) {
	if r.URL.EscapedPath() != "/restconf/data/interfaces/interface=ethernet%201%2F1%2F2/ethernet/poe" && r.URL.EscapedPath() != "/restconf/data/interfaces/interface=ethernet%201%2F1%2F2/ethernet/poe/config" {
		w.WriteHeader(404)
		return
	}
	if r.Method == "GET" {
		json.NewEncoder(w).Encode(map[string]any{"icx-openconfig-if-poe-aug:poe": map[string]any{"config": map[string]any{"enabled": s.poe}, "state": map[string]any{"enabled": true, "power-used": "7000.0", "power-class": 4}}})
		return
	}
	if r.Method == "PUT" {
		var body struct {
			Config struct {
				Enabled *bool `json:"enabled"`
			} `json:"config"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Config.Enabled == nil {
			w.WriteHeader(400)
			return
		}
		s.poe = *body.Config.Enabled
		s.writes++
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(400)
}
