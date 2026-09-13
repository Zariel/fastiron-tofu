package lldp

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
	native := func(enabled bool) string {
		if enabled {
			return "ver 09.0.10kT213\nend"
		}
		return "ver 09.0.10kT213\nno lldp run\nend"
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
	server.HandleFunc("/lldp/config", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, `{"openconfig-lldp:config":{"enabled":%t}}`, enabled)
			return
		}
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(405)
			return
		}
		writes++
		enabled = false
		http.Error(w, "injected partial mutation failure", http.StatusInternalServerError)
	})
	device, err := fastiron.New(fastiron.Config{
		Host: "switch", Transport: "restconf", Persistence: "after_each_write",
		RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
		SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}

	observed, err := applyEnabled(context.Background(), device, "", false)
	if err == nil || observed == nil || *observed {
		t.Fatalf("partial failure lost: observed=%v error=%v", observed, err)
	}
	mu.Lock()
	beforeSaves, beforeWrites, stillSaved := saves, writes, saved
	mu.Unlock()
	if beforeSaves != 0 || beforeWrites != 1 || !stillSaved {
		t.Fatal("saved configuration after a failed mutation")
	}

	observed, err = applyEnabled(context.Background(), device, "", false)
	if err != nil || observed == nil || *observed {
		t.Fatalf("retry: observed=%v error=%v", observed, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if saved || enabled || writes != 1 || saves != 1 {
		t.Fatalf("retry: running=%t saved=%t writes=%d saves=%d", enabled, saved, writes, saves)
	}
}

func TestGlobalVerification(t *testing.T) {
	for _, failure := range []string{"", "echo only", "unowned change", "initial disagreement"} {
		t.Run(failure, func(t *testing.T) {
			var mu sync.Mutex
			enabled, cached, saved := true, true, true
			if failure == "initial disagreement" {
				cached = false
			}
			port := "no lldp enable transmit ports ethe 1/1/12"
			savedPort := port
			writes, saves := 0, 0
			native := func(enabled bool, port string) string {
				text := "ver 09.0.10kT213\n" + port + "\n"
				if !enabled {
					text += "no lldp run\n"
				}
				return text + "end"
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
					return native(enabled, port)
				case "show configuration":
					return native(saved, savedPort)
				case "write memory":
					saves++
					saved, savedPort = enabled, port
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/lldp/config", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					fmt.Fprintf(w, `{"openconfig-lldp:config":{"enabled":%t}}`, cached)
					return
				}
				if r.Method != http.MethodPatch {
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
					return
				}
				writes++
				cached = false
				if failure != "echo only" {
					enabled = false
				}
				if failure == "unowned change" {
					port = "no lldp enable ports ethe 1/1/12"
				}
				w.WriteHeader(http.StatusNoContent)
			})
			device, err := fastiron.New(fastiron.Config{
				Host: "switch", Transport: "restconf", Persistence: "after_each_write",
				RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
				SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}

			observed, err := applyEnabled(context.Background(), device, "", false)
			mu.Lock()
			defer mu.Unlock()
			if (err != nil) != (failure != "") || observed == nil || *observed != enabled {
				t.Fatalf("observed=%v native=%t error=%v", observed, enabled, err)
			}
			if failure == "" {
				if saved || enabled || saves != 1 || writes != 1 || port != "no lldp enable transmit ports ethe 1/1/12" {
					t.Fatal("global mutation failed to persist or preserve port mode")
				}
				return
			}
			if saves != 0 || !saved || savedPort != "no lldp enable transmit ports ethe 1/1/12" {
				t.Fatal("unverified configuration was saved")
			}
			if failure == "initial disagreement" && writes != 0 {
				t.Fatal("mutated configuration despite inconsistent observations")
			}
		})
	}
}
