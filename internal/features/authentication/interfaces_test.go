package authentication

import (
	"reflect"
	"testing"
)

func TestAuthenticationInterfaces(t *testing.T) {
	input := `aaa authentication dot1x default radius
interface ethernet 1/1/8
 dot1x enable ethe 1/1/7
!
authentication
 auth-default-vlan 3055
 dot1x enable
 dot1x enable ethe 1/1/9 to 1/1/10
 dot1x port-control auto ethe 1/1/10
 mac-authentication enable
 mac-authentication enable ethe 1/1/9
!
`
	got, _, err := nativeAuthenticationInterfaces(input)
	want := map[string]interfaceConfig{
		"ethernet 1/1/9":  {Dot1XEnabled: true, MACEnabled: true, PortControl: "force-authorized"},
		"ethernet 1/1/10": {Dot1XEnabled: true, PortControl: "auto"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("interfaces=%v error=%v", got, err)
	}
}

func TestAuthenticationPortControl(t *testing.T) {
	got, _, err := nativeAuthenticationInterfaces("dot1x port-control force-unauthorized ethernet 2/1/3 2/1/5\n")
	want := map[string]interfaceConfig{
		"ethernet 2/1/3": {PortControl: "force-unauthorized"},
		"ethernet 2/1/5": {PortControl: "force-unauthorized"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("interfaces=%v error=%v", got, err)
	}
}

func TestAuthenticationPortErrors(t *testing.T) {
	for name, line := range map[string]string{
		"conflicting modes": "dot1x port-control auto ethe 1/1/9\ndot1x port-control force-unauthorized ethe 1/1/9",
		"missing ports":     "dot1x port-control auto",
		"unknown mode":      "dot1x port-control unknown ethe 1/1/9",
		"incomplete range":  "dot1x port-control auto ethe 1/1/9 to",
		"reversed range":    "dot1x port-control auto ethe 1/1/10 to 1/1/9",
		"excessive range":   "dot1x port-control auto ethe 1/1/1 to 1/1/9999999",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := nativeAuthenticationInterfaces(line); err == nil {
				t.Fatal("accepted malformed native authentication configuration")
			}
		})
	}
}
