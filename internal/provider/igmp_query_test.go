package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestOpenTofuGlobalIGMPQuery(t *testing.T) {
	s := newSwitch(t)
	previous := s.server.Config.Handler
	s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/igmp-mld-snooping/global") && r.Method == http.MethodGet {
			// RESTCONF can retain values absent from native configuration.
			fmt.Fprint(w, `{"icx-igmp-mld-snooping:global":{"igmp":{"config":{"querier-mode":"active","version":3}}}}`)
			return
		}
		previous.ServeHTTP(w, r)
	})
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`data "fastiron_igmp_snooping" "test" {}
output "igmp" { value = data.fastiron_igmp_snooping.test }
`)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	var observed struct {
		Mode    string `json:"querier_mode"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(run(0, "output", "-json", "igmp")), &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Mode != "disabled" || observed.Version != 2 {
		t.Fatalf("query did not report native defaults: %+v", observed)
	}
	run(0, "plan", "-detailed-exitcode", "-no-color")
	if s.writes != 0 {
		t.Fatal("query changed switch configuration")
	}
}
