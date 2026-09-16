package provider

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func dnsConfiguration(servers map[string]bool) string {
	var b strings.Builder
	for _, address := range slices.Sorted(maps.Keys(servers)) {
		family := "ip"
		if strings.Contains(address, ":") {
			family = "ipv6"
		}
		b.WriteString(family + " dns server-address " + address + "\n")
	}
	return b.String()
}

func (s *testSwitch) dnsREST(w http.ResponseWriter, r *http.Request) {
	const base = "/restconf/data/system/dns"
	if r.Method == "GET" && r.URL.Path == base {
		entries := []any{}
		servers := s.dns
		if s.cachedDNS != nil {
			servers = s.cachedDNS
		}
		for address := range servers {
			entries = append(entries, map[string]any{"address": address, "config": map[string]any{"address": address}})
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-system:dns": map[string]any{"servers": map[string]any{"server": entries}}})
		return
	}
	if s.ignoreDNSWrites {
		w.WriteHeader(http.StatusNoContent)
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
		if s.corruptDNS {
			s.ethernet["description"] = "unexpected DNS side effect"
		}
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

func TestOpenTofuDNSPreservation(t *testing.T) {
	s := newSwitch(t)
	s.corruptDNS = true
	write, run, base := tofuFixture(t, s)
	write("main.tf", base+`resource "fastiron_ip_dns_server" "test" { address = "192.0.2.53" }`)
	run(0, "init", "-no-color")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "DNS server operation changed unrelated native configuration") {
		t.Fatalf("missing preservation error: %s", out)
	}
	if out := run(0, "state", "show", "fastiron_ip_dns_server.test"); !strings.Contains(out, "persistence_pending = true") {
		t.Fatalf("partial creation is not addressable for retry: %s", out)
	}
	s.mu.Lock()
	running, saved := s.dns["192.0.2.53"], s.startupDNS["192.0.2.53"]
	savedName := s.startupEthernet["description"]
	s.mu.Unlock()
	if !running || saved || savedName != "manual port" {
		t.Fatalf("unexpected persistence: running=%v saved=%v saved port name=%v", running, saved, savedName)
	}

	// Repair the independently owned interface before retrying persistence.
	s.mu.Lock()
	s.corruptDNS = false
	s.ethernet["description"] = "manual port"
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")
	s.mu.Lock()
	persisted := s.startupDNS["192.0.2.53"]
	s.mu.Unlock()
	if !persisted {
		t.Fatal("repaired DNS configuration was not persisted")
	}
}

func TestOpenTofuDNSCache(t *testing.T) {
	s := newSwitch(t)
	s.dns = map[string]bool{"192.0.2.53": true, "2001:db8::53": true}
	s.cachedDNS = map[string]bool{"192.0.2.54": true}
	write, run, base := tofuFixture(t, s)
	configuration := `resource "fastiron_ip_dns_server" "test" { address = "192.0.2.53" }
data "fastiron_ip_dns_servers" "test" { depends_on = [fastiron_ip_dns_server.test] }
output "dns" { value = data.fastiron_ip_dns_servers.test.addresses }
`
	write("main.tf", base+configuration)
	run(0, "init", "-no-color")
	run(0, "import", "-no-color", "fastiron_ip_dns_server.test", "ip dns server-address 192.0.2.53")
	run(0, "apply", "-refresh-only", "-auto-approve", "-no-color")
	if got := strings.TrimSpace(run(0, "output", "-json", "dns")); got != `["192.0.2.53","2001:db8::53"]` {
		t.Fatalf("DNS discovery did not use native configuration: %s", got)
	}
	s.mu.Lock()
	if s.writes != 0 || len(s.startupDNS) != 0 {
		s.mu.Unlock()
		t.Fatal("import or query changed configuration")
	}
	delete(s.dns, "192.0.2.53")
	s.cachedDNS["192.0.2.53"] = true
	s.ignoreDNSWrites = true
	s.mu.Unlock()

	run(2, "plan", "-detailed-exitcode", "-no-color")
	if out := run(1, "apply", "-auto-approve", "-no-color"); !strings.Contains(out, "DNS server configuration did not converge") {
		t.Fatalf("cached success hid absent native server: %s", out)
	}
	s.mu.Lock()
	if len(s.startupDNS) != 0 {
		s.mu.Unlock()
		t.Fatal("failed DNS convergence was saved")
	}
	s.ignoreDNSWrites = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	write("main.tf", base)
	run(0, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	defer s.mu.Unlock()
	want := map[string]bool{"2001:db8::53": true}
	if !maps.Equal(s.dns, want) || !maps.Equal(s.startupDNS, want) {
		t.Fatalf("DNS deletion did not preserve IPv6: running=%v startup=%v", s.dns, s.startupDNS)
	}
}
