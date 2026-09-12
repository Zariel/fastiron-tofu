package provider

import (
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
