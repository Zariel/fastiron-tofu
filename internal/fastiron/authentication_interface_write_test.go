package fastiron

import (
	"reflect"
	"testing"
)

func TestAuthenticationMembershipOwnership(t *testing.T) {
	// Authentication owns the target's implicit default-VLAN exclusion, while
	// changes to other ports and tagged memberships must remain observable.
	before := []string{"default-vlan-id 4090", "vlan 4090 name DEFAULT-VLAN by port", "no untagged ethe 1/1/10", "!", "vlan 20 by port", "tagged ethe 1/1/9", "!"}
	after := []string{"default-vlan-id 4090", "vlan 4090 name DEFAULT-VLAN by port", "no untagged ethe 1/1/9 to 1/1/10", "!", "vlan 20 by port", "tagged ethe 1/1/9", "!"}
	want := []string{"default-vlan-id 4090", "vlan 4090 name DEFAULT-VLAN by port", "no untagged ethernet 1/1/10", "!", "vlan 20 by port", "tagged ethe 1/1/9", "!"}
	for _, lines := range [][]string{before, after} {
		got, err := authenticationNeighbors(lines, "ethernet 1/1/9", true)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("neighbors=%v error=%v", got, err)
		}
	}
	for _, line := range []string{"untagged ethe 1/1/9", "untagged ethe 1/1/8 to 1/1/10"} {
		if _, err := authenticationNeighbors([]string{"vlan 20 by port", line, "!"}, "ethernet 1/1/9", true); err == nil {
			t.Fatal("accepted conflicting explicit untagged membership")
		}
	}
}
