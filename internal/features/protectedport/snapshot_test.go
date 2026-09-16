package protectedport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestSnapshot(t *testing.T) {
	var reads atomic.Int32
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			if reads.Add(1)%2 == 0 {
				return "ver 09.0.10k\nend"
			}
			return "ver 09.0.10k\nlag GUEST static id 11\ninterface lag 11\n protected-port\nend"
		default:
			t.Errorf("query issued command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /interfaces", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"lag 11","config":{"name":"lag 11"}}]}}`)
	})
	server.HandleFunc("GET /protectedport", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"icx-openconfig-pp:protectedport":{}}`)
	})
	device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := read(context.Background(), device, "lag 11")
	if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		t.Fatal(err)
	}
	// Either complete snapshot is valid; neither has an existing, unprotected LAG.
	if err == nil && !observed.enabled {
		t.Fatal("query combined parent existence with policy from another configuration")
	}
}
