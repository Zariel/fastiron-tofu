package provider

import (
	"encoding/json"
	"net/http"
)

func (s *testSwitch) veREST(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/restconf/data/openconfig-interfaces:interfaces/interface/ve 53/config/description" {
		if s.ve == nil {
			w.WriteHeader(404)
			return
		}
		if r.Method == "DELETE" {
			s.ve["description"] = ""
			s.writes++
			w.WriteHeader(204)
			return
		}
		var body struct {
			Description *string `json:"openconfig-interfaces:description"`
		}
		if r.Method != "PUT" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Description == nil {
			w.WriteHeader(400)
			return
		}
		s.ve["description"] = *body.Description
		s.writes++
		w.WriteHeader(204)
		return
	}
	if r.Method == "DELETE" && r.URL.EscapedPath() == "/restconf/data/interfaces/interface=ve%2053" {
		s.ve = nil
		s.veChild = false
		s.writes++
		w.WriteHeader(204)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/restconf/data/interfaces" {
		var body struct {
			Interface []struct {
				Name   string         `json:"name"`
				Config map[string]any `json:"config"`
				Routed struct {
					Config struct {
						VLAN int `json:"vlan"`
					} `json:"config"`
				} `json:"openconfig-vlan:routed-vlan"`
			} `json:"interface"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Interface) != 1 {
			w.WriteHeader(400)
			return
		}
		entry := body.Interface[0]
		if entry.Name != "ve 53" || entry.Config["name"] != entry.Name || entry.Config["type"] != "iana-if-type:l3ipvlan" || entry.Routed.Config.VLAN != 53 {
			w.WriteHeader(400)
			return
		}
		if _, exists := s.running[53]; !exists {
			w.WriteHeader(400)
			return
		}
		s.ve = entry.Config
		s.writes++
		w.WriteHeader(201)
		return
	}
	w.WriteHeader(404)
}
