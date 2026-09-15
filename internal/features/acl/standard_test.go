package acl

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestStandardRead(t *testing.T) {
	current, unowned, err := nativeStandard(configtest.Parse(t, nativeFixture(`ver 09.0.10kT213
ip access-list standard 90
 sequence 30 permit any
 sequence 10 permit 192.0.2.0 0.0.0.255
 sequence 20 deny host 198.51.100.10
ip access-list extended NEIGHBOR
 sequence 10 permit ip any any
interface ethernet 1/1/9
 ip access-group 90 in
end`)), "90")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]standardRule{10: {10, "permit", "192.0.2.0/24"}, 20: {20, "deny", "198.51.100.10/32"}, 30: {30, "permit", "any"}}
	if current == nil || current.Name != "90" || !maps.Equal(current.Rules, want) {
		t.Fatalf("standard ACL = %+v", current)
	}
	if !slices.Equal(unowned, []string{"ver 09.0.10kT213", "ip access-list extended NEIGHBOR", " sequence 10 permit ip any any", "interface ethernet 1/1/9", " ip access-group 90 in", "end"}) {
		t.Fatalf("unowned = %q", unowned)
	}
}

func TestStandardSource(t *testing.T) {
	for name, tc := range map[string]struct {
		fields []string
		want   string
	}{
		"host zero":     {[]string{"host", "0.0.0.0"}, "0.0.0.0/32"},
		"wildcard any":  {[]string{"0.0.0.0", "255.255.255.255"}, "any"},
		"wildcard host": {[]string{"192.0.2.1", "0.0.0.0"}, "192.0.2.1/32"},
		"prefix":        {[]string{"192.0.2.0/24"}, "192.0.2.0/24"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := standardSource(tc.fields)
			if err != nil || got != tc.want {
				t.Fatalf("source=%q error=%v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestStandardOwnership(t *testing.T) {
	for name, rule := range map[string]string{
		"logging":       "sequence 10 permit any log",
		"remark":        "remark operator rule",
		"noncontiguous": "sequence 10 permit 192.0.2.0 0.255.0.255",
		"duplicate":     "sequence 10 permit any\n sequence 10 deny any",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := nativeStandard(configtest.Parse(t, nativeFixture("ip access-list standard 90\n "+rule+"\nend")), "90"); err == nil {
				t.Fatal("unrepresented native configuration accepted")
			}
		})
	}
}

func TestStandardAbsence(t *testing.T) {
	current, _, err := nativeStandard(configtest.Parse(t, nativeFixture("ver 09.0.10kT213\nip access-list standard 91\n sequence 10 permit any\nend")), "90")
	if err != nil || current != nil {
		t.Fatalf("absent ACL = %+v, error=%v", current, err)
	}
	current, _, err = nativeStandard(configtest.Parse(t, nativeFixture("ver 09.0.10kT213\nip access-list standard 90\nend")), "90")
	if err != nil || current == nil || len(current.Rules) != 0 {
		t.Fatalf("empty ACL = %+v, error=%v", current, err)
	}
}

func TestBannerACL(t *testing.T) {
	input := "ver 09.0.10k\nbanner motd $\nip access-list standard EDGE\n sequence 10 permit any\n$\nend"
	acl, unowned, err := nativeStandard(configtest.Parse(t, input), "EDGE")
	if err != nil || acl != nil {
		t.Fatalf("banner interpreted as an ACL: %v, %v", acl, err)
	}
	if strings.Join(unowned, "\n") != input {
		t.Fatal("banner text changed")
	}
}
