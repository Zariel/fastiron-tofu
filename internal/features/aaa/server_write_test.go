package aaa

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestNativeAAAServer(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		desired    server
		key        bool
	}{
		{"radius defaults", "radius-server host 192.0.2.53", server{Kind: "radius", Address: "192.0.2.53", AuthPort: 1812, AcctPort: 1813, Purpose: "default"}, false},
		{"radius configured", "radius-server host 192.0.2.53 auth-port 1912 acct-port 1913 accounting-only key 2 opaque", server{Kind: "radius", Address: "192.0.2.53", AuthPort: 1912, AcctPort: 1913, Purpose: "accounting-only"}, true},
		{"tacacs defaults", "tacacs-server host 192.0.2.53", server{Kind: "tacacs", Address: "192.0.2.53", AuthPort: 49, Purpose: "default"}, false},
		{"tacacs configured", "tacacs-server host 192.0.2.53  auth-port 2112 authorization-only key 2 opaque", server{Kind: "tacacs", Address: "192.0.2.53", AuthPort: 2112, Purpose: "authorization-only"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := "aaa authentication login default local\nno aaa accounting commands 0 default\nradius-server retransmit 4\n" + tc.line + "\ntacacs-server host 192.0.2.54\n"
			got, neighbors, err := nativeAAA(input, tc.desired)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.server != tc.desired || got.hasKey != tc.key {
				t.Fatal("native server metadata differs")
			}
			want := []string{"aaa authentication login default local", "no aaa accounting commands 0 default", "radius-server retransmit 4", "tacacs-server host 192.0.2.54"}
			if !reflect.DeepEqual(neighbors, want) {
				t.Fatal("neighbor configuration was lost or reordered")
			}
		})
	}
}

func TestNativeAAAOwnership(t *testing.T) {
	desired := server{Kind: "radius", Address: "192.0.2.53"}
	for _, options := range []string{
		"ssl-auth-port 2083 profile secure", "auth-port 1812 dot1x", "auth-port 1812 key 2 opaque mac-auth", "auth-port 1812 no-login", "auth-port 1812 port-only", "auth-port 1812 web-auth", "auth-port 1812 auth-port 1912", "acct-port 0", "key", "authorization-only",
	} {
		t.Run(options, func(t *testing.T) {
			_, _, err := nativeAAA("radius-server host 192.0.2.53 "+options, desired)
			if err == nil {
				t.Fatal("accepted configuration outside RESTCONF ownership")
			}
			if strings.Contains(err.Error(), "opaque") {
				t.Fatal("error exposed native key")
			}
		})
	}
}

func TestAAAWritesRequireOptIn(t *testing.T) {
	d := &fastiron.Device{}
	s := server{Kind: "radius", Address: "192.0.2.53", AuthPort: 1812, AcctPort: 1813, Purpose: "default"}
	for _, present := range []bool{true, false} {
		if _, err := applyServer(context.Background(), d, s, nil, present); err == nil || !strings.Contains(err.Error(), "allow_aaa_changes") {
			t.Fatal("AAA write was not rejected before accessing the device")
		}
	}
}

func TestAAAKeyValidation(t *testing.T) {
	d, err := fastiron.New(fastiron.Config{Host: "switch.invalid", Transport: "restconf", Persistence: "never", AllowAAAChanges: true, RESTCONF: &restconf.Config{URL: "https://switch.invalid/restconf/data", Timeout: time.Second, Username: ""}})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"radius", "tacacs"} {
		s := server{Kind: kind, Address: "192.0.2.53", AuthPort: 49, Purpose: "default"}
		if kind == "radius" {
			s.AuthPort, s.AcctPort = 1812, 1813
		}
		limit := 64
		if kind == "tacacs" {
			limit = 32
		}
		for _, key := range []string{"", "test key", "test\nkey", "test\x1bkey", strings.Repeat("x", limit+1)} {
			if _, err := applyServer(context.Background(), d, s, &key, true); err == nil || !strings.Contains(err.Error(), "AAA key") {
				t.Fatal("invalid key was not rejected before device access")
			}
		}
	}
}

func TestNativeAAAOrder(t *testing.T) {
	s := server{Kind: "radius", Address: "192.0.2.53"}
	first, neighbors, err := nativeAAA("radius-server host 192.0.2.53\nradius-server host 192.0.2.54", s)
	if err != nil {
		t.Fatal(err)
	}
	moved, after, err := nativeAAA("radius-server host 192.0.2.54\nradius-server host 192.0.2.53", s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(neighbors, after) || first.position == moved.position {
		t.Fatal("server reordering was indistinguishable from an unchanged neighbor configuration")
	}
}

func TestKeylessTACACS(t *testing.T) {
	d, err := fastiron.New(fastiron.Config{Host: "switch.invalid", Transport: "restconf", Persistence: "never", AllowAAAChanges: true, RESTCONF: &restconf.Config{URL: "https://switch.invalid/restconf/data", Timeout: time.Second, Username: ""}})
	if err != nil {
		t.Fatal(err)
	}
	s := server{Kind: "tacacs", Address: "192.0.2.53", AuthPort: 49, Purpose: "default"}
	if _, err := applyServer(context.Background(), d, s, nil, true); err == nil || !strings.Contains(err.Error(), "keyless configuration") {
		t.Fatal("unsupported keyless TACACS write was not rejected before device access")
	}
}
