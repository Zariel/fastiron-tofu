package lag

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

func TestDeleteObservationDeadline(t *testing.T) {
	var present atomic.Bool
	present.Store(true)
	server := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			time.Sleep(180 * time.Millisecond)
			if present.Load() {
				return "ver 09.0.10k\nlag test static id 53\n ports ethe 1/1/7\nend"
			}
			return "ver 09.0.10k\nend"
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("GET /interfaces", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(120 * time.Millisecond)
		entries := `{"name":"ethernet 1/1/7","config":{"name":"ethernet 1/1/7","type":"iana-if-type:ethernetCsmacd"}}`
		if present.Load() {
			entries += `,{"name":"lag 53","config":{"name":"lag 53","type":"iana-if-type:ieee8023adLag"},"openconfig-if-aggregate:aggregation":{"config":{"lag-type":"STATIC","openconfig-if-aggregate-aug:lag-name":"test"}}}`
		}
		fmt.Fprintf(w, `{"openconfig-interfaces:interfaces":{"interface":[%s]}}`, entries)
	})
	server.HandleFunc("GET /interfaces/interface=lag 53/aggregation/switched-vlan", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-vlan:switched-vlan":{"config":{"access-vlan":1}}}`)
	})
	server.HandleFunc("DELETE /stp/interfaces/interface=lag 53", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	server.HandleFunc("DELETE /interfaces/interface=lag 53", func(w http.ResponseWriter, _ *http.Request) {
		present.Store(false)
		w.WriteHeader(http.StatusNoContent)
	})
	device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: 200 * time.Millisecond}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}

	if err := deleteLAG(context.Background(), device, 53); err != nil {
		t.Fatal(err)
	}
	if present.Load() {
		t.Fatal("native LAG remains")
	}
}

func TestObservationCancellation(t *testing.T) {
	device := lagDevice(t, "ver 09.0.10k\nend", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/7","config":{"name":"ethernet 1/1/7","type":"iana-if-type:ethernetCsmacd"}}]}}`)
	}, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := waitLAG(ctx, device, 53, func(*config) bool { return false }); !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("retry budget did not stop polling independently of caller: %v (caller: %v)", err, ctx.Err())
	}
	cancel()
	if _, err := waitLAG(ctx, device, 53, func(*config) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation was ignored: %v", err)
	}
}
