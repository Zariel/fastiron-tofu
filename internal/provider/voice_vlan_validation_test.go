package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestOpenTofuVoiceVLANValidation(t *testing.T) {
	s := newSwitch(t)
	previous := s.server.Config.Handler
	s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ethernet/config") && r.Method == http.MethodGet {
			fmt.Fprint(w, `{"openconfig-if-ethernet:config":{}}`)
			return
		}
		previous.ServeHTTP(w, r)
	})
	write, run, base := tofuFixture(t, s)
	write("main.tf", base)
	run(0, "init", "-no-color")
	for _, tc := range []struct {
		name       string
		id         int64
		diagnostic string
	}{
		{"ethernet 1/1/12", 0, "Invalid voice VLAN"},
		{"ethernet 1/1/12", 4096, "Invalid voice VLAN"},
		{"lag 1", 3053, "Invalid voice VLAN interface"},
		{"ethernet 01/1/12", 3053, "Invalid voice VLAN interface"},
		{"ethernet 1/1/12", 1, ""},
		{"ethernet 2/1/48", 4095, ""},
	} {
		write("main.tf", base+fmt.Sprintf("resource \"fastiron_interface_voice_vlan\" \"test\" {\n interface = %q\n vlan_id = %d\n}\n", tc.name, tc.id))
		code := 0
		if tc.diagnostic != "" {
			code = 1
		}
		output := run(code, "plan", "-no-color")
		if !strings.Contains(output, tc.diagnostic) {
			t.Fatalf("missing diagnostic: %s", output)
		}
	}
	if s.writes != 0 {
		t.Fatal("planning changed switch configuration")
	}
}
