package dscptrust

import (
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config/configtest"
)

func TestNative(t *testing.T) {
	configuration := "ver 09.0.10k\nno flow-control\ninterface ethernet 1/1/12\n port-name EDGE\n trust dscp\n disable\ninterface lag 11\n trust dscp\nend"
	for _, name := range []string{"ethernet 1/1/12", "lag 11"} {
		got, err := parse(configtest.Parse(t, configuration), name)
		if err != nil || !got.enabled || got.symmetricFlowControl {
			t.Fatalf("%s: state=%+v error=%v", name, got, err)
		}
	}
	got, err := parse(configtest.Parse(t, configuration), "ethernet 1/1/12")
	want := "ver 09.0.10k\nno flow-control\n port-name EDGE\n disable\ninterface lag 11\n trust dscp\nend"
	if err != nil || strings.Join(got.unowned, "\n") != want {
		t.Fatalf("unowned=%q error=%v", got.unowned, err)
	}
	absent, err := parse(configtest.Parse(t, configuration), "ethernet 1/1/11")
	if err != nil || absent.enabled || strings.Join(absent.unowned, "\n") != configuration {
		t.Fatalf("default=%+v error=%v", absent, err)
	}
}

func TestSymmetricFlowControl(t *testing.T) {
	for _, command := range []string{"symmetrical-flow-control enable", "symmetrical-flow-control enable all-priorities"} {
		configuration := "ver 09.0.10k\n" + command + "\nend"
		got, err := parse(configtest.Parse(t, configuration), "ethernet 1/1/12")
		if err != nil || !got.symmetricFlowControl || got.validate(true) == nil || got.validate(false) != nil {
			t.Fatalf("state=%+v error=%v", got, err)
		}
		if strings.Join(got.unowned, "\n") != configuration {
			t.Fatal("flow control was not preserved as unrelated configuration")
		}
	}
}

func TestMalformedNative(t *testing.T) {
	for _, configuration := range []string{
		"ver 09.0.10k\ninterface lag 11\n trust dscp extra\nend",
		"ver 09.0.10k\ninterface lag 11\n trust dscp\n trust dscp\nend",
		"ver 09.0.10k\ninterface lag 11\n trust dscp\ninterface lag 11\nend",
	} {
		if _, err := parse(configtest.Parse(t, configuration), "lag 11"); err == nil {
			t.Fatalf("accepted %q", configuration)
		}
	}
}
