package poe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestPolicyWrite(t *testing.T) {
	for _, tc := range []struct {
		name, before, after, body string
		desired                   config.PoEPolicy
		corrupt, failure          bool
	}{
		{name: "cached limit after drift", before: " no inline power\n", after: " inline power priority 2 power-limit 18000\n", body: `{"config":{"enabled":true,"priority":2,"power-limit":18000}}`, desired: config.PoEPolicy{Enabled: true, Priority: 2, PowerLimitMilliwatts: 18000}},
		{name: "limit to class", before: " inline power priority 2 power-limit 18000\n", after: " inline power priority 1 power-by-class 2\n", body: `{"config":{"enabled":true,"priority":1,"power-by-class":2}}`, desired: config.PoEPolicy{Enabled: true, Priority: 1, PowerByClass: 2}},
		{name: "restore defaults", before: " inline power priority 2 power-limit 18000\n", body: `{"config":{"enabled":true,"priority":3,"power-by-class":0}}`, desired: config.PoEPolicy{Enabled: true, Priority: 3}},
		{name: "disable allocation", before: " inline power priority 2 power-limit 18000\n", after: " no inline power\n", body: `{"config":{"enabled":false,"priority":3,"power-by-class":0}}`, desired: config.PoEPolicy{Priority: 3}},
		{name: "lost allocation", before: " no inline power\n", after: " inline power priority 2\n", body: `{"config":{"enabled":true,"priority":2,"power-limit":18000}}`, desired: config.PoEPolicy{Enabled: true, Priority: 2, PowerLimitMilliwatts: 18000}, failure: true},
		{name: "unrelated mutation", before: " no inline power\n", after: " inline power priority 2 power-limit 18000\n", body: `{"config":{"enabled":true,"priority":2,"power-limit":18000}}`, desired: config.PoEPolicy{Enabled: true, Priority: 2, PowerLimitMilliwatts: 18000}, corrupt: true, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			running, saved := tc.before, tc.before
			writes, saves := 0, 0
			native := func(policy string) string {
				extra := " port-name phone\n"
				if tc.corrupt && writes > 0 {
					extra = ""
				}
				return "ver 09.0.10k\ninterface ethernet 1/1/11\n no inline power\ninterface ethernet 1/1/12\n" + extra + policy + "end"
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
					return native(running)
				case "show configuration":
					return native(saved)
				case "write memory":
					saves++
					saved = running
					return "Write startup-config done."
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodGet {
					// The cached desired value must not conceal a native drift or failed write.
					fmt.Fprintf(w, `{"icx-openconfig-if-poe-aug:poe":%s}`, tc.body)
					return
				}
				var body, expected any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if err := json.Unmarshal([]byte(tc.body), &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(body, expected) {
					t.Errorf("policy request=%v; want %v", body, expected)
				}
				if r.URL.EscapedPath() != "/interfaces/interface=ethernet%201%2F1%2F12/ethernet/poe/config" {
					t.Errorf("wrong endpoint %s", r.URL.EscapedPath())
				}
				writes++
				if r.Method == http.MethodPut {
					running = tc.after
				}
				w.WriteHeader(204)
			})
			device, err := fastiron.New(fastiron.Config{Host: server.SSHAddress, Transport: "restconf", Persistence: "after_each_write", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := applyPort(context.Background(), device, "ethernet 1/1/12", tc.desired)
			if (err != nil) != tc.failure || observed == nil {
				t.Fatalf("observed=%+v, error=%v", observed, err)
			}
			mu.Lock()
			if tc.failure {
				if saves != 0 || saved != tc.before {
					t.Errorf("failed workflow saved: saves=%d saved=%q", saves, saved)
				}
			} else if saves != 1 || saved != tc.after || observed.PoEPolicy != tc.desired {
				t.Errorf("policy did not persist: saves=%d saved=%q observed=%+v", saves, saved, observed)
			}
			mu.Unlock()
			if tc.failure {
				return
			}

			if _, err := applyPort(context.Background(), device, "ethernet 1/1/12", tc.desired); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != 1 {
				t.Fatalf("converged policy rewritten %d times", writes)
			}
		})
	}
}

func TestInvalidPolicy(t *testing.T) {
	for _, policy := range []config.PoEPolicy{
		{Enabled: true, Priority: 0},
		{Enabled: true, Priority: 4},
		{Enabled: true, Priority: 3, PowerByClass: -1},
		{Enabled: true, Priority: 3, PowerByClass: 5},
		{Enabled: true, Priority: 3, PowerLimitMilliwatts: 999},
		{Enabled: true, Priority: 3, PowerLimitMilliwatts: 95001},
		{Enabled: true, Priority: 3, PowerByClass: 2, PowerLimitMilliwatts: 18000},
		{Priority: 1},
		{Priority: 3, PowerByClass: 2},
		{Priority: 3, PowerLimitMilliwatts: 18000},
	} {
		if _, err := applyPort(context.Background(), nil, "ethernet 1/1/12", policy); err == nil {
			t.Errorf("invalid policy accepted: %+v", policy)
		}
	}
}
