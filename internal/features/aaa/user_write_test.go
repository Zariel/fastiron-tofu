package aaa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestNativeUser(t *testing.T) {
	for _, tc := range []struct {
		line string
		want account
	}{
		{"username reader privilege 5 password $6$opaque", account{Username: "reader", Privilege: 5}},
		{"username reader password $6$opaque", account{Username: "reader", Privilege: 0}},
	} {
		got, neighbors, err := userConfiguration(nativeFixture("username operator password $6$other\n"+tc.line), "reader")
		if err != nil || got == nil || got.user != tc.want || !got.hasPassword {
			t.Fatalf("native metadata error=%v", err)
		}
		if len(neighbors) != 1 || neighbors[0] != "username operator password $6$other" {
			t.Fatal("neighbor configuration was lost")
		}
	}
}

func TestUserOwnership(t *testing.T) {
	for _, line := range []string{
		"username reader expires 30", "username reader access-time 09:00 to 17:00", "no username reader enable", "username reader privilege 5 password opaque\nusername reader expires 30", "username reader privilege 1 password opaque",
	} {
		if _, _, err := userConfiguration(nativeFixture(line), "reader"); err == nil {
			t.Fatal("accepted native user options outside ownership")
		}
	}
}

func TestUserTransportAccount(t *testing.T) {
	d, err := fastiron.New(fastiron.Config{Host: "switch.invalid", Transport: "restconf", Persistence: "never", AllowAAAChanges: true, RESTCONF: &restconf.Config{URL: "https://switch.invalid/restconf/data", Timeout: time.Second, Username: "automation"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, present := range []bool{true, false} {
		for _, name := range []string{"automation", "AUTOMATION"} {
			if _, err := applyUser(context.Background(), d, account{Username: name}, "test-password", present); err == nil || !strings.Contains(err.Error(), "transport account") {
				t.Fatal("transport account mutation was not rejected before device access")
			}
		}
	}
}

func TestBannerUser(t *testing.T) {
	input := "ver 09.0.10k\nbanner motd $\nusername reader privilege 5 password opaque\n$\nend"
	user, _, err := userConfiguration(input, "reader")
	if err != nil || user != nil {
		t.Fatalf("banner interpreted as an account: %v, %v", user, err)
	}
}
