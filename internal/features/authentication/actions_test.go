package authentication

import (
	"encoding/json"
	"testing"
)

func TestGlobalActions(t *testing.T) {
	for _, tc := range []struct{ native, want string }{
		{"auth-fail-action restricted-vlan", `{"fail-action":{"fail-action":"restricted-vlan"}}`},
		{"auth-timeout-action success", `{"timeout-action":{"success":true}}`},
		{"auth-timeout-action failure", `{"timeout-action":{"failure":true}}`},
		{"auth-timeout-action critical-vlan", `{"timeout-action":{"critical-vlan":true}}`},
		{"reauth-period 2000", `{}`},
	} {
		t.Run(tc.native, func(t *testing.T) {
			actions, err := globalActions([]string{tc.native})
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(actions)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("payload=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestGlobalActionOwnership(t *testing.T) {
	for _, lines := range [][]string{
		{"auth-fail-action restricted-vlan voice voice-vlan"},
		{"auth-timeout-action critical-vlan voice voice-vlan"},
		{"auth-timeout-action unknown"},
		{"auth-timeout-action success", "auth-timeout-action failure"},
	} {
		if _, err := globalActions(lines); err == nil {
			t.Fatal("accepted an action outside the supported ownership boundary")
		}
	}
}
