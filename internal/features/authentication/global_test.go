package authentication

import (
	"slices"
	"testing"
)

func TestGlobalConfiguration(t *testing.T) {
	current, _, err := nativeGlobal(nativeFixture(`ver 09.0.10kT213
!
authentication
 auth-order mac-auth dot1x
 auth-default-vlan 3055
 restricted-vlan 3056
 critical-vlan 3057
 voice-vlan 3058
 auth-fail-action restricted-vlan
 auth-timeout-action critical-vlan
 dot1x enable
 dot1x enable ethe 1/1/9 to 1/1/10
 dot1x guest-vlan 3059
 dot1x port-control auto ethe 1/1/10
 mac-authentication enable
 mac-authentication enable ethe 1/1/10
 mac-authentication dot1x-disable
 mac-authentication dot1x-override
 max-sessions 15
 re-authentication
 reauth-period 120
!
interface ethernet 1/1/9
 max-sessions 3
!
end
`))
	if err != nil {
		t.Fatal(err)
	}
	want := globalConfig{
		Dot1XEnabled: true, MACEnabled: true, AuthOrder: "mac-auth dot1x",
		DefaultVLAN: 3055, RestrictedVLAN: 3056, CriticalVLAN: 3057, VoiceVLAN: 3058, GuestVLAN: 3059,
		FailureAction: "restricted-vlan", TimeoutAction: "critical-vlan", MaxSessions: 15,
		Reauthentication: true, MACDot1XDisable: true, MACDot1XOverride: true,
	}
	if current != want {
		t.Fatalf("configuration = %+v; want %+v", current, want)
	}
}

func TestGlobalDefaults(t *testing.T) {
	for _, native := range []string{
		"ver 09.0.10kT213\ninterface ethernet 1/1/9\n max-sessions 15\nend\n",
		"ver 09.0.10kT213\nauthentication\n dot1x enable ethe 1/1/10\n mac-authentication enable ethe 1/1/10\nend\n",
	} {
		current, _, err := nativeGlobal(nativeFixture(native))
		if err != nil {
			t.Fatal(err)
		}
		if current != (globalConfig{AuthOrder: "dot1x mac-auth", MaxSessions: 2}) {
			t.Fatalf("defaults = %+v", current)
		}
	}
}

func TestNativeGlobalActions(t *testing.T) {
	for _, action := range []string{"success", "failure", "critical-vlan", "critical-vlan voice voice-vlan"} {
		t.Run(action, func(t *testing.T) {
			current, _, err := nativeGlobal(nativeFixture("authentication\n auth-fail-action restricted-vlan voice voice-vlan\n auth-timeout-action " + action + "\nend\n"))
			if err != nil {
				t.Fatal(err)
			}
			if current.TimeoutAction != action || current.FailureAction != "restricted-vlan voice voice-vlan" {
				t.Fatalf("actions = %+v", current)
			}
		})
	}
}

func TestInvalidGlobalConfiguration(t *testing.T) {
	for name, native := range map[string]string{
		"order":      " auth-order mac-auth\n",
		"vlan":       " auth-default-vlan invalid\n",
		"sessions":   " max-sessions 0\n",
		"flag":       " re-authentication false\n",
		"duplicate":  " auth-timeout-action success\n auth-timeout-action failure\n",
		"failure":    " auth-fail-action unknown\n",
		"timeout":    " auth-timeout-action unknown\n",
		"incomplete": " dot1x\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := nativeGlobal(nativeFixture("authentication\n" + native + "end\n")); err == nil {
				t.Fatal("invalid native configuration accepted")
			}
		})
	}
}

func TestGlobalOwnership(t *testing.T) {
	_, unowned, err := nativeGlobal(nativeFixture("ver 09.0.10kT213\nauthentication\n auth-default-vlan 3055\n dot1x enable\n dot1x guest-vlan 3058\n dot1x enable ethe 1/1/10\n reauth-period 120\ninterface ethernet 1/1/9\n port-name neighbor\nend"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ver 09.0.10kT213", " dot1x guest-vlan 3058", " dot1x enable ethe 1/1/10", " reauth-period 120", "interface ethernet 1/1/9", " port-name neighbor", "end"}
	if !slices.Equal(unowned, want) {
		t.Fatalf("unowned = %q; want %q", unowned, want)
	}
}

func TestBannerAuthentication(t *testing.T) {
	input := "ver 09.0.10k\nbanner motd $\nauthentication\n dot1x enable\n$\nend"
	observed, _, err := nativeGlobal(input)
	if err != nil || observed.Dot1XEnabled {
		t.Fatalf("banner interpreted as authentication: %+v, %v", observed, err)
	}
}
