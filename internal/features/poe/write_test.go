package poe

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
	enabled, saved := true, true
	writes, saves := 0, 0
	native := func(value bool) string {
		if value {
			return "ver 09.0.10k\ninterface ethernet 1/1/12\nend"
		}
		return "ver 09.0.10k\ninterface ethernet 1/1/12\n no inline power\nend"
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
			return native(enabled)
		case "show configuration":
			return native(saved)
		case "write memory":
			saves++
			saved = enabled
			return "Write startup-config done."
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/interfaces/interface=ethernet%201%2F1%2F12/ethernet/poe", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, `{"icx-openconfig-if-poe-aug:poe":{"config":{"enabled":%t}}}`, enabled)
			return
		}
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(405)
			return
		}
		writes++
		enabled = false
		http.Error(w, "partial PoE mutation", 500)
	})
	device, err := fastiron.New(fastiron.Config{Host: "switch", Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}

	observed, err := applyPort(context.Background(), device, "ethernet 1/1/12", false)
	if err == nil || observed == nil || observed.Enabled {
		t.Fatalf("partial error lost: observed=%+v error=%v", observed, err)
	}
	mu.Lock()
	if saves != 0 || writes != 1 || !saved || enabled {
		t.Errorf("failed mutation persisted: writes=%d saves=%d running=%t saved=%t", writes, saves, enabled, saved)
	}
	mu.Unlock()

	observed, err = applyPort(context.Background(), device, "ethernet 1/1/12", false)
	if err != nil || observed == nil || observed.Enabled {
		t.Fatalf("retry: observed=%+v error=%v", observed, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 1 || saves != 1 || saved || enabled {
		t.Fatalf("retry repeated mutation or failed to save: writes=%d saves=%d running=%t saved=%t", writes, saves, enabled, saved)
	}
}
