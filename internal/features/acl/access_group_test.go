package acl

import (
	"slices"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestAccessGroupOwnership(t *testing.T) {
	output := `ver 09.0.10kT213
ip access-list standard 90
 sequence 10 permit any
ipv6 access-list V6
 sequence 10 permit ipv6 any any
lag uplink dynamic id 1
 ports ethe 1/2/1 to 1/2/2
interface lag 1
 port-name UPLINK
 ip access-group 90 in
 ip access-group 90 out
 ipv6 access-group V6 in
end`
	view, err := nativeAccessGroup(configtest.Parse(t, nativeFixture(output)), accessGroupKey{"lag 1", "ip", "in"})
	if err != nil {
		t.Fatal(err)
	}
	if view.ACL != "90" || !view.Available[[2]string{"ip", "90"}] || !view.Available[[2]string{"ipv6", "V6"}] {
		t.Fatalf("view=%+v", view)
	}
	want := []string{"ver 09.0.10kT213", "ip access-list standard 90", " sequence 10 permit any", "ipv6 access-list V6", " sequence 10 permit ipv6 any any", "lag uplink dynamic id 1", " ports ethe 1/2/1 to 1/2/2", " port-name UPLINK", " ip access-group 90 out", " ipv6 access-group V6 in", "end"}
	if !slices.Equal(view.Unowned, want) {
		t.Fatalf("unowned=%q", view.Unowned)
	}
}

func TestAccessGroupUnsupported(t *testing.T) {
	for name, lines := range map[string]string{
		"logging":   " ip access-group 90 in logging enable",
		"duplicate": " ip access-group 90 in\n ip access-group 91 in",
		"malformed": " ip access-group 90",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := nativeAccessGroup(configtest.Parse(t, nativeFixture("ver 09.0.10kT213\ninterface ethernet 1/1/9\n"+lines+"\nend")), accessGroupKey{"ethernet 1/1/9", "ip", "in"})
			if err == nil {
				t.Fatal("ambiguous or unrepresented binding accepted")
			}
		})
	}
}

func TestAccessGroupDefaultHeader(t *testing.T) {
	k := accessGroupKey{"ethernet 1/1/9", "ip", "in"}
	before, err := nativeAccessGroup(configtest.Parse(t, nativeFixture("ver 09.0.10kT213\nend")), k)
	if err != nil {
		t.Fatal(err)
	}
	after, err := nativeAccessGroup(configtest.Parse(t, nativeFixture("ver 09.0.10kT213\ninterface ethernet 1/1/9\n ip access-group 90 in\nend")), k)
	if err != nil {
		t.Fatal(err)
	}
	if after.ACL != "90" || !slices.Equal(before.Unowned, after.Unowned) {
		t.Fatal("default interface header treated as unrelated configuration")
	}
}

func TestVLANBindingOwnership(t *testing.T) {
	output := `ver 09.0.10kT213
vlan 100 name USERS by port
 tagged ethe 1/1/9
 ip access-group 90 in
 ip access-group 91 out
 ipv6 access-group V6 in
vlan 1000 name NEIGHBOR by port
 ip access-group 91 in
ip access-list standard 90
 sequence 10 permit any
end`
	view, err := nativeAccessGroup(configtest.Parse(t, nativeFixture(output)), accessGroupKey{"vlan 100", "ip", "in"})
	if err != nil {
		t.Fatal(err)
	}
	if view.ACL != "90" {
		t.Fatalf("binding=%q", view.ACL)
	}
	want := []string{
		"ver 09.0.10kT213", "vlan 100 name USERS by port", " tagged ethe 1/1/9",
		" ip access-group 91 out", " ipv6 access-group V6 in",
		"vlan 1000 name NEIGHBOR by port", " ip access-group 91 in",
		"ip access-list standard 90", " sequence 10 permit any", "end",
	}
	if !slices.Equal(view.Unowned, want) {
		t.Fatalf("unowned=%q", view.Unowned)
	}
}

func TestVLANBindingPortSubset(t *testing.T) {
	output := "ver 09.0.10kT213\nvlan 100 by port\n ip access-group 90 in ethernet 1/1/9\nend"
	if _, err := nativeAccessGroup(configtest.Parse(t, nativeFixture(output)), accessGroupKey{"vlan 100", "ip", "in"}); err == nil {
		t.Fatal("port-specific VLAN binding was treated as an entire-VLAN binding")
	}
}
