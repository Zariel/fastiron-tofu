package provider

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Model independently observed leaf-list merge and keyed deletion behavior.
// The caller holds the simulator lock across this request.
func (s *testSwitch) membershipREST(w http.ResponseWriter, r *http.Request) {
	const portPath = "/restconf/data/interfaces/interface=ethernet%201%2F1%2F2/ethernet/switched-vlan"
	if r.Method == "GET" && r.URL.EscapedPath() == portPath {
		access := 1
		tags := []int{}
		for id, tagging := range s.memberships {
			if tagging == "untagged" {
				access = id
			} else {
				tags = append(tags, id)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-vlan:switched-vlan": map[string]any{"config": map[string]any{"access-vlan": access, "trunk-vlans": tags}}})
		return
	}
	if r.Method == "PATCH" && r.URL.EscapedPath() == portPath {
		var body struct {
			Port struct {
				Config struct {
					Access *int  `json:"access-vlan"`
					Trunks []int `json:"trunk-vlans"`
				} `json:"config"`
			} `json:"openconfig-vlan:switched-vlan"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		if body.Port.Config.Access != nil {
			for id, tagging := range s.memberships {
				if tagging == "untagged" {
					delete(s.memberships, id)
				}
			}
			s.memberships[*body.Port.Config.Access] = "untagged"
		}
		for _, id := range body.Port.Config.Trunks {
			s.memberships[id] = "tagged"
		}
		s.writes++
		w.WriteHeader(204)
		return
	}
	if r.Method == "DELETE" && strings.HasPrefix(r.URL.EscapedPath(), portPath) {
		suffix := strings.TrimPrefix(r.URL.EscapedPath(), portPath)
		if suffix == "/config/access-vlan" {
			for id, tagging := range s.memberships {
				if tagging == "untagged" {
					delete(s.memberships, id)
				}
			}
		} else if strings.HasPrefix(suffix, "/config/trunk-vlans=") {
			id, err := strconv.Atoi(strings.TrimPrefix(suffix, "/config/trunk-vlans="))
			if err != nil {
				w.WriteHeader(400)
				return
			}
			delete(s.memberships, id)
		} else {
			w.WriteHeader(404)
			return
		}
		s.writes++
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(400)
}
