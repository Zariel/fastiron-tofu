package fastiron

import (
	"reflect"
	"testing"
)

func TestNativeAAAPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        AAAPolicy
	}{
		{"absent dot1x", "aaa authentication login default local\n", AAAPolicy{LoginMethods: []string{"local"}}},
		{"explicit none and grouped ignores", "aaa authentication login default radius local\naaa authentication dot1x default none\naaa authorization coa enable\naaa authorization coa ignore modify-acl dm-request\n", AAAPolicy{LoginMethods: []string{"radius", "local"}, Dot1XDefault: "none", CoAEnabled: true, CoAIgnore: []string{"dm-request", "modify-acl"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := "aaa authentication web-server default local\n" + tc.input + "username operator password opaque"
			got, neighbors, err := nativeAAAPolicy(input)
			if err != nil || got == nil || !reflect.DeepEqual(*got, tc.want) {
				t.Fatalf("policy=%v error=%v", got, err)
			}
			if !reflect.DeepEqual(neighbors, []string{"aaa authentication web-server default local", "username operator password opaque"}) {
				t.Fatalf("unowned configuration=%v", neighbors)
			}
		})
	}
}

func TestNativeAAAPolicyOwnership(t *testing.T) {
	for name, extra := range map[string]string{
		"privilege mode":   "aaa authentication login privilege-mode",
		"fallback":         "aaa authentication dot1x default radius none",
		"unknown action":   "aaa authorization coa ignore unknown",
		"duplicate action": "aaa authorization coa ignore dm-request dm-request",
		"duplicate login":  "aaa authentication login default radius",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := nativeAAAPolicy("aaa authentication login default local\n" + extra); err == nil {
				t.Fatal("accepted unsupported native ownership")
			}
		})
	}
}
