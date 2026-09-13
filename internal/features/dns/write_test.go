package dns

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestPartialWrite(t *testing.T) {
	var mu sync.Mutex
	present, saved := false, false
	writes, saves := 0, 0
	native := func(present bool) string {
		if present {
			return "ver 09.0.10k\nip dns server-address 192.0.2.53\nend"
		}
		return "ver 09.0.10k\nend"
	}
	server := testswitch.New(t, func(command string) string {
		mu.Lock()
		defer mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return native(present)
		case "show configuration":
			return native(saved)
		case "write memory":
			saves++
			saved = present
			return "Write startup-config done."
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/system/dns", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		entries := ""
		if present {
			entries = `{"address":"192.0.2.53","config":{"address":"192.0.2.53"}}`
		}
		fmt.Fprintf(w, `{"openconfig-system:dns":{"servers":{"server":[%s]}}}`, entries)
	})
	server.HandleFunc("/system/dns/servers", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		writes++
		present = true
		http.Error(w, "partial DNS mutation", 500)
	})
	device, err := fastiron.New(fastiron.Config{
		Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write",
		RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
		SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}

	observed, err := applyServer(context.Background(), device, "192.0.2.53", true)
	if err == nil || !observed {
		t.Fatalf("partial write: observed=%t error=%v", observed, err)
	}
	mu.Lock()
	if writes != 1 || saves != 0 || saved || !present {
		t.Errorf("failed mutation persisted: writes=%d saves=%d running=%t saved=%t", writes, saves, present, saved)
	}
	mu.Unlock()

	observed, err = applyServer(context.Background(), device, "192.0.2.53", true)
	if err != nil || !observed {
		t.Fatalf("retry: observed=%t error=%v", observed, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 1 || saves != 1 || !saved {
		t.Errorf("retry: writes=%d saves=%d saved=%t", writes, saves, saved)
	}
}
