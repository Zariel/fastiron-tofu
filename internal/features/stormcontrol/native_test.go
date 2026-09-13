package stormcontrol

import (
	"strings"
	"testing"
)

func TestNative(t *testing.T) {
	configuration := "ver 09.0.10k\nrate-limit-log 3\ninterface ethernet 1/1/12\n port-name TEST\n broadcast limit 111 kbps\n multicast limit 222 kbps\n unknown-unicast limit 333 kbps\n disable\ninterface lag 11\n broadcast limit 444\nend"
	got, err := parse(configuration, "ethernet 1/1/12")
	if err != nil {
		t.Fatal(err)
	}
	want := policy{unit: "kbps", limits: map[string]int64{"broadcast": 111, "multicast": 222, "unknown-unicast": 333}}
	if !got.policy.equal(want) || got.options {
		t.Fatalf("observation=%+v", got)
	}
	unowned := "ver 09.0.10k\nrate-limit-log 3\n port-name TEST\n disable\ninterface lag 11\n broadcast limit 444\nend"
	if strings.Join(got.unowned, "\n") != unowned {
		t.Fatalf("unowned=%q", got.unowned)
	}
	lag, err := parse(configuration, "lag 11")
	if err != nil || lag.policy.unit != "pps" || lag.policy.limits["broadcast"] != 444 {
		t.Fatalf("LAG=%+v error=%v", lag, err)
	}
	absent, err := parse(configuration, "ethernet 1/1/13")
	if err != nil || absent.policy.unit != "" || len(absent.policy.limits) != 0 || strings.Join(absent.unowned, "\n") != configuration {
		t.Fatalf("default=%+v error=%v", absent, err)
	}
}

func TestNativeOptions(t *testing.T) {
	for _, command := range []string{"broadcast limit 111 kbps log", "broadcast limit 333 kbps threshold 444 action port-shutdown 3", "broadcast limit 555 pps threshold 777 action port-shutdown 3"} {
		got, err := parse("ver 09.0.10k\ninterface ethernet 1/1/12\n "+command+"\nend", "ethernet 1/1/12")
		if err != nil || !got.options || got.writable() == nil {
			t.Fatalf("%s: observation=%+v error=%v", command, got, err)
		}
	}
}

func TestMalformedNative(t *testing.T) {
	for _, configuration := range []string{
		"ver 09.0.10k\ninterface lag 11\n broadcast limit 111",
		"ver 09.0.10k\ninterface lag 11\n broadcast limit\nend",
		"ver 09.0.10k\ninterface lag 11\n broadcast limit 0\nend",
		"ver 09.0.10k\ninterface lag 11\n broadcast limit 111 mbps\nend",
		"ver 09.0.10k\ninterface lag 11\n broadcast limit 111 kbps\n multicast limit 222\nend",
		"ver 09.0.10k\ninterface lag 11\n broadcast limit 111\n broadcast limit 222\nend",
		"ver 09.0.10k\ninterface lag 11\n broadcast limit 111\ninterface lag 11\n multicast limit 222\nend",
	} {
		if _, err := parse(configuration, "lag 11"); err == nil {
			t.Fatalf("accepted %q", configuration)
		}
	}
}
