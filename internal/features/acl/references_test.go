package acl

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestIPReferences(t *testing.T) {
	for name, tc := range map[string]struct {
		config string
		bound  bool
	}{
		"binding":        {"interface ethernet 1/1/9\n ipv6 access-group TEST in\n", true},
		"access class":   {"ipv6 access-class TEST\n", true},
		"route map":      {"route-map example permit 10\n match ipv6 address OTHER TEST\n", true},
		"MLD":            {"interface ve 5\n ipv6 mld access-group TEST\n", true},
		"boundary":       {"interface ve 5\n ipv6 multicast-boundary TEST\n", true},
		"neighbor":       {"interface ve 5\n ipv6 pim neighbor-filter TEST\n", true},
		"join policy":    {"ipv6 router pim\n jp-policy TEST\n", true},
		"join RP":        {"ipv6 router pim vrf blue\n jp-policy 2001:db8::1 TEST\n", true},
		"RP":             {"ipv6 router pim\n rp-address 2001:db8::1 TEST\n", true},
		"anycast RP":     {"ipv6 router pim\n anycast-rp 2001:db8::1 TEST\n", true},
		"register":       {"ipv6 router pim\n accept-register TEST\n", true},
		"SSM":            {"ipv6 router pim\n ssm-enable range TEST\n", true},
		"slow path":      {"ipv6 router pim\n slow-path-forwarding filter TEST\n", true},
		"other family":   {"router pim\n jp-policy TEST\n", false},
		"context reset":  {"ipv6 router pim\nrouter pim\n jp-policy TEST\n", false},
		"different name": {"ipv6 router pim\n accept-register TEST2\n", false},
		"prefix list":    {"route-map example permit 10\n match ipv6 address prefix-list TEST\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			const acl = "ipv6 access-list TEST\n sequence 10 permit ipv6 any any\n"
			before := absentStandard + acl + tc.config + neighbor
			after := absentStandard + tc.config + neighbor
			s := &standardSwitch{running: before}
			s.restACLs = map[string]any{"openconfig-acl:acl-sets": map[string]any{"acl-set": []any{
				map[string]any{"name": "TEST", "type": "openconfig-acl:ACL_IPV6", "acl-entries": map[string]any{"acl-entry": []any{map[string]int{"sequence-id": 10}}}},
			}}}
			device := s.device(t, func(w http.ResponseWriter, r *http.Request) {
				s.running = after
				w.WriteHeader(http.StatusNoContent)
			})

			err := deleteIP(context.Background(), device, ipv6ACL, "TEST")
			if !tc.bound {
				if err != nil || s.startup != after {
					t.Fatalf("unrelated configuration blocked deletion: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "remove native ACL references") {
				t.Fatalf("reference accepted: %v", err)
			}
			if s.writes != 0 || s.saves != 0 || s.running != before || s.startup != before {
				t.Fatal("referenced ACL was changed")
			}
		})
	}
}
