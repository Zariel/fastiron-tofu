package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestOpenTofuIGMPValidation(t *testing.T) {
	s := newSwitch(t)
	previous := s.server.Config.Handler
	s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/igmp-mld-snooping/vlans") && r.Method == http.MethodGet {
			fmt.Fprint(w, `{"icx-igmp-mld-snooping:vlans":{}}`)
			return
		}
		previous.ServeHTTP(w, r)
	})
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for _, tc := range []struct{ fields, diagnostic string }{
		{`querier_mode = "disabled"`, "Invalid IGMP mode"},
		{`querier_mode = ""`, "Invalid IGMP mode"},
		{`version = 0`, "Invalid IGMP version"},
		{`version = 1`, "Invalid IGMP version"},
		{`version = 4`, "Invalid IGMP version"},
	} {
		write("main.tf", base+"resource \"fastiron_vlan_igmp_snooping\" \"test\" {\n vlan_id = 53\n"+tc.fields+"\n}\n")
		output := run(1, "plan", "-no-color")
		if !strings.Contains(output, tc.diagnostic) {
			t.Fatalf("missing diagnostic: %s", output)
		}
	}
	for _, fields := range []string{"", `querier_mode = "active"`, `version = 3`, "querier_mode = \"passive\"\n version = 2"} {
		write("main.tf", base+"resource \"fastiron_vlan_igmp_snooping\" \"test\" {\n vlan_id = 53\n"+fields+"\n}\n")
		run(0, "plan", "-no-color")
	}
	if s.writes != 0 {
		t.Fatal("planning changed switch configuration")
	}
}

func TestOpenTofuGlobalIGMPValidation(t *testing.T) {
	s := newSwitch(t)
	previous := s.server.Config.Handler
	s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/igmp-mld-snooping/global") && r.Method == http.MethodGet {
			fmt.Fprint(w, `{"icx-igmp-mld-snooping:global":{"igmp":{}}}`)
			return
		}
		previous.ServeHTTP(w, r)
	})
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")

	for _, tc := range []struct{ fields, diagnostic string }{
		{`querier_mode = ""`, "Invalid global IGMP mode"},
		{`querier_mode = "querier"`, "Invalid global IGMP mode"},
		{`version = 0`, "Invalid global IGMP version"},
		{`version = 1`, "Invalid global IGMP version"},
		{`version = 4`, "Invalid global IGMP version"},
	} {
		write("main.tf", base+"resource \"fastiron_igmp_snooping\" \"test\" {\n"+tc.fields+"\n}\n")
		output := run(1, "plan", "-no-color")
		if !strings.Contains(output, tc.diagnostic) {
			t.Fatalf("missing diagnostic: %s", output)
		}
	}
	for _, fields := range []string{"", `querier_mode = "disabled"`, `querier_mode = "active"`, `version = 3`, "querier_mode = \"passive\"\n version = 2"} {
		write("main.tf", base+"resource \"fastiron_igmp_snooping\" \"test\" {\n"+fields+"\n}\n")
		run(0, "plan", "-no-color")
	}

	write("main.tf", base+"resource \"fastiron_igmp_snooping\" \"test\" {}\n")
	run(0, "plan", "-out=defaults.plan", "-no-color")
	var plan struct {
		Changes []struct {
			Address string `json:"address"`
			Change  struct {
				After struct {
					Mode    string `json:"querier_mode"`
					Version int64  `json:"version"`
				} `json:"after"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal([]byte(run(0, "show", "-json", "defaults.plan")), &plan); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range plan.Changes {
		if change.Address != "fastiron_igmp_snooping.test" {
			continue
		}
		found = true
		if change.Change.After.Mode != "disabled" || change.Change.After.Version != 2 {
			t.Fatalf("unexpected global defaults: %+v", change.Change.After)
		}
	}
	if !found {
		t.Fatal("plan omitted global IGMP resource")
	}
	if s.writes != 0 {
		t.Fatal("planning changed switch configuration")
	}
}
