package fastiron

import (
	"context"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestNativeUser(t *testing.T) {
	for _, tc := range []struct {
		line string
		want User
	}{
		{"username reader privilege 5 password $6$opaque", User{Username: "reader", Privilege: 5}},
		{"username reader password $6$opaque", User{Username: "reader", Privilege: 0}},
	} {
		got, neighbors, err := userConfiguration("username operator password $6$other\n"+tc.line, "reader")
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
		if _, _, err := userConfiguration(line, "reader"); err == nil {
			t.Fatal("accepted native user options outside ownership")
		}
	}
}

func TestUserTransportAccount(t *testing.T) {
	d := &Device{config: Config{AllowAAAChanges: true, RESTCONF: &restconf.Config{Username: "automation"}}, discoveryGate: make(chan struct{}, 1)}
	for _, present := range []bool{true, false} {
		for _, name := range []string{"automation", "AUTOMATION"} {
			if _, err := d.ApplyUser(context.Background(), User{Username: name}, "test-password", present); err == nil || !strings.Contains(err.Error(), "transport account") {
				t.Fatal("transport account mutation was not rejected before device access")
			}
		}
	}
}
