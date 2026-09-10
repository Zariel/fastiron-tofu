package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

type ospfSwitch struct {
	areas, startup                               map[string][]string
	numeric                                      bool
	areaOptions, interfaceOptions, hiddenBinding bool
}

func ospfConfiguration(areas map[string][]string, areaOptions, interfaceOptions, hiddenBinding bool) string {
	var b strings.Builder
	if len(areas) > 0 {
		b.WriteString("router ospf\n")
	}
	for _, id := range slices.Sorted(maps.Keys(areas)) {
		fmt.Fprintf(&b, " area %s\n", id)
		if id == "0.0.0.53" && areaOptions {
			b.WriteString(" area 0.0.0.53 range 198.51.100.0/24\n")
		}
	}
	b.WriteString("!\n")
	for _, id := range slices.Sorted(maps.Keys(areas)) {
		for _, name := range areas[id] {
			fmt.Fprintf(&b, "interface %s\n ip ospf area %s\n", name, id)
			if id == "0.0.0.53" && interfaceOptions {
				b.WriteString(" ip ospf network point-to-point\n")
			}
			b.WriteString("!\n")
		}
	}
	if hiddenBinding {
		b.WriteString("interface ve 54\n ip ospf area 53\n!\n")
	}
	return b.String()
}

func (s *ospfSwitch) rest(w http.ResponseWriter, r *http.Request) {
	const protocols = "/restconf/data/network-instances/network-instance=default-vrf/protocols"
	const collection = protocols + "/protocol=OSPF,icx-ospf/ospfv2/areas"
	endpoint := r.URL.EscapedPath()
	if r.Method == "GET" && endpoint == collection {
		areas := []any{}
		for id, names := range s.areas {
			var identifier any = id
			if s.numeric && id == "0.0.0.53" {
				identifier = 53
			}
			bindings := []any{}
			for _, name := range names {
				bindings = append(bindings, map[string]any{"id": name, "config": map[string]any{"id": name}})
			}
			areas = append(areas, map[string]any{"identifier": identifier, "config": map[string]any{"identifier": identifier}, "interfaces": map[string]any{"interface": bindings}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-network-instance:areas": map[string]any{"area": areas}})
		return
	}
	if r.Method == "PATCH" && endpoint == protocols {
		var body struct {
			Protocols struct {
				Protocol []struct {
					OSPF struct {
						Areas struct {
							Area []struct {
								ID string `json:"identifier"`
							} `json:"area"`
						} `json:"areas"`
					} `json:"ospfv2"`
				} `json:"protocol"`
			} `json:"protocols"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		for _, protocol := range body.Protocols.Protocol {
			for _, area := range protocol.OSPF.Areas.Area {
				if _, exists := s.areas[area.ID]; !exists {
					s.areas[area.ID] = []string{}
				}
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rest, ok := strings.CutPrefix(endpoint, collection+"/area=")
	if !ok {
		http.NotFound(w, r)
		return
	}
	key, suffix, _ := strings.Cut(rest, "/")
	id, _ := url.PathUnescape(key)
	if s.numeric && id == "0.0.0.53" {
		http.NotFound(w, r)
		return
	}
	if id == "53" {
		id = "0.0.0.53"
	}
	names, exists := s.areas[id]
	if !exists {
		http.NotFound(w, r)
		return
	}
	if r.Method == "DELETE" && suffix == "" {
		// Model destructive parent deletion so missing ownership guards lose bindings.
		delete(s.areas, id)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == "POST" && suffix == "interfaces" {
		var body struct {
			Interface []struct {
				ID string `json:"id"`
			} `json:"interface"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		for _, entry := range body.Interface {
			if !slices.Contains(names, entry.ID) {
				names = append(names, entry.ID)
			}
		}
		s.areas[id] = names
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == "DELETE" && strings.HasPrefix(suffix, "interfaces/interface=") {
		name, _ := url.PathUnescape(strings.TrimPrefix(suffix, "interfaces/interface="))
		s.areas[id] = slices.DeleteFunc(names, func(v string) bool { return v == name })
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "unsupported OSPF operation", 400)
}

func TestOpenTofuOSPF(t *testing.T) {
	s := newSwitch(t)
	s.ospf = &ospfSwitch{areas: map[string][]string{"0.0.0.0": {"ve 5"}}, startup: map[string][]string{"0.0.0.0": {"ve 5"}}}
	write, run, base := tofuFixture(t, s)
	area := `resource "fastiron_router_ospf_area" "test" { area_id = "0.0.0.53" }
`
	binding := `resource "fastiron_router_ospf_interface" "test" {
 area_id = fastiron_router_ospf_area.test.area_id
 interface = "ve 53"
}
`
	discovery := `data "fastiron_ospf_areas" "test" { depends_on = [fastiron_router_ospf_interface.test] }
output "areas" { value = data.fastiron_ospf_areas.test.areas }
`
	check := func(want map[string][]string) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !maps.EqualFunc(s.ospf.areas, want, slices.Equal[[]string]) || !maps.EqualFunc(s.ospf.startup, want, slices.Equal[[]string]) {
			t.Fatalf("areas running=%v startup=%v want=%v", s.ospf.areas, s.ospf.startup, want)
		}
	}
	write("main.tf", base+area+binding+discovery)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {"ve 53"}})
	if output := strings.TrimSpace(run(0, "output", "-json", "areas")); output != `{"0.0.0.0":{"interfaces":["ve 5"]},"0.0.0.53":{"interfaces":["ve 53"]}}` {
		t.Fatalf("area discovery=%s", output)
	}
	s.mu.Lock()
	s.ospf.numeric = true
	s.mu.Unlock()
	run(0, "plan", "-detailed-exitcode", "-no-color")
	run(0, "state", "rm", "fastiron_router_ospf_interface.test", "fastiron_router_ospf_area.test")
	run(0, "import", "-no-color", "fastiron_router_ospf_area.test", "0.0.0.53")
	run(0, "import", "-no-color", "fastiron_router_ospf_interface.test", "0.0.0.53|ve 53")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	run(0, "state", "rm", "fastiron_router_ospf_interface.test")
	write("main.tf", base)
	run(1, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {"ve 53"}})
	write("main.tf", base+area+binding+discovery)
	run(0, "import", "-no-color", "fastiron_router_ospf_interface.test", "0.0.0.53|ve 53")
	run(0, "apply", "-auto-approve", "-no-color")

	s.mu.Lock()
	s.ospf.areas["0.0.0.53"] = []string{}
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {"ve 53"}})
	write("main.tf", base+area)
	s.mu.Lock()
	s.ospf.interfaceOptions = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {"ve 53"}})
	s.mu.Lock()
	s.ospf.interfaceOptions = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {}})

	write("main.tf", base)
	for _, condition := range []string{"options", "hidden binding"} {
		s.mu.Lock()
		s.ospf.areaOptions = condition == "options"
		s.ospf.hiddenBinding = condition == "hidden binding"
		s.mu.Unlock()
		run(1, "apply", "-auto-approve", "-no-color")
		check(map[string][]string{"0.0.0.0": {"ve 5"}, "0.0.0.53": {}})
	}
	s.mu.Lock()
	s.ospf.areaOptions, s.ospf.hiddenBinding = false, false
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[string][]string{"0.0.0.0": {"ve 5"}})
	run(0, "plan", "-detailed-exitcode", "-no-color")
}
