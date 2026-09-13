package stormcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestApply(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		initial, cached, desired       map[string]limit
		ignore, corrupt, fail, options bool
	}{
		{name: "reject logging adoption", initial: map[string]limit{"broadcast": {111, true}}, desired: map[string]limit{"broadcast": {111, true}}, options: true},
		{name: "create", desired: map[string]limit{"broadcast": {111, false}, "multicast": {222, false}}},
		{name: "stale cache", initial: map[string]limit{"broadcast": {999, false}}, cached: map[string]limit{"broadcast": {444, false}}, desired: map[string]limit{"broadcast": {444, false}}},
		{name: "recreate removed policy", cached: map[string]limit{"broadcast": {111, true}}, desired: map[string]limit{"broadcast": {111, true}}},
		{name: "native-only deletion", initial: map[string]limit{"broadcast": {111, true}}},
		{name: "remove class", initial: map[string]limit{"broadcast": {111, true}, "multicast": {222, true}}, desired: map[string]limit{"multicast": {222, true}}},
		{name: "change unit", initial: map[string]limit{"broadcast": {111, true}, "multicast": {222, true}}, desired: map[string]limit{"broadcast": {333, false}, "multicast": {444, false}}},
		{name: "retry partial failure", initial: map[string]limit{"broadcast": {111, true}}, desired: map[string]limit{"broadcast": {333, false}}, fail: true},
		{name: "false acknowledgement", desired: map[string]limit{"broadcast": {111, false}}, ignore: true},
		{name: "unrelated mutation", desired: map[string]limit{"broadcast": {111, false}}, corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			native, cached := maps.Clone(tc.initial), maps.Clone(tc.cached)
			if native == nil {
				native = map[string]limit{}
			}
			if cached == nil {
				cached = map[string]limit{}
			}
			neighbor, writes, failed := "PHONE", 0, false
			server := testswitch.New(t, func(command string) string {
				mu.Lock()
				defer mu.Unlock()
				switch command {
				case "skip-page-display":
					return ""
				case "show version":
					return "SW: Version 09.0.10kT213"
				case "show running-config":
					text := "ver 09.0.10kT213\ninterface ethernet 1/1/12\n port-name " + neighbor + "\n disable\n"
					for _, class := range classes {
						rate, exists := native[class]
						if !exists {
							continue
						}
						text += fmt.Sprintf(" %s limit %d", class, rate.Rate)
						if rate.KBPS {
							text += " kbps"
						}
						if tc.options {
							text += " log"
						}
						text += "\n"
					}
					return text + "end"
				default:
					t.Errorf("unexpected command %q", command)
					return "% Invalid input"
				}
			})
			server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				target := "/openconfig-interfaces:interfaces/interface=ethernet 1/1/12/config/storm_control_config"
				if r.Method == http.MethodGet {
					switch r.URL.Path {
					case "/interfaces":
						fmt.Fprint(w, `{"openconfig-interfaces:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12"}}]}}`)
					case target:
						json.NewEncoder(w).Encode(map[string]any{"icx-openconfig-stormcontrol:storm_control_config": cached})
					default:
						t.Errorf("unexpected GET %s", r.URL.Path)
						w.WriteHeader(404)
					}
					return
				}
				writes++
				switch {
				case r.Method == http.MethodPatch && r.URL.Path == target:
					var body struct {
						Rates map[string]limit `json:"storm_control_config"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					for class, rate := range body.Rates {
						for other, current := range native {
							if other != class && current.KBPS != rate.KBPS {
								w.WriteHeader(500)
								return
							}
						}
						if cached[class] != rate && !tc.ignore {
							native[class] = rate
						}
						cached[class] = rate
					}
				case r.Method == http.MethodDelete && (r.URL.Path == target || strings.HasPrefix(r.URL.Path, target+"/")):
					for class := range cached {
						if r.URL.Path == target || r.URL.Path == target+"/"+class {
							delete(cached, class)
							if !tc.ignore {
								delete(native, class)
							}
						}
					}
				default:
					t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				if tc.corrupt {
					neighbor = "CHANGED"
				}
				if tc.fail && !failed && native["broadcast"] == (limit{333, false}) {
					failed = true
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(204)
			})
			device, err := fastiron.New(fastiron.Config{Host: "switch", Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second}, SSH: &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			desired := policy{limits: map[string]int64{}}
			for class, rate := range tc.desired {
				desired.limits[class] = rate.Rate
				desired.unit = "pps"
				if rate.KBPS {
					desired.unit = "kbps"
				}
			}
			observed, err := apply(context.Background(), device, "ethernet 1/1/12", desired)
			if tc.options {
				mu.Lock()
				defer mu.Unlock()
				if err == nil || !strings.Contains(err.Error(), "cannot be preserved") || writes != 0 {
					t.Fatalf("options adopted: writes=%d error=%v", writes, err)
				}
				return
			}
			if tc.ignore || tc.corrupt {
				want := "did not converge"
				if tc.corrupt {
					want = "unrelated configuration"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected %s, got %v", want, err)
				}
				return
			}
			if tc.fail {
				if err == nil || observed == nil || observed.unit != "pps" || observed.limits["broadcast"] != 333 {
					t.Fatalf("lost partial state: %+v %v", observed, err)
				}
				_, err = apply(context.Background(), device, "ethernet 1/1/12", desired)
			}
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			before := writes
			mu.Unlock()
			if _, err := apply(context.Background(), device, "ethernet 1/1/12", desired); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !maps.Equal(native, tc.desired) || neighbor != "PHONE" || writes != before {
				t.Fatalf("native=%v neighbor=%s writes=%d before=%d", native, neighbor, writes, before)
			}
		})
	}
}

func TestRateRange(t *testing.T) {
	for _, tc := range []struct {
		unit  string
		rate  int64
		valid bool
	}{
		{"pps", 1, true},
		{"pps", 8388607, true},
		{"pps", 8388608, false},
		{"kbps", 1, true},
		{"kbps", 1000000, true},
		{"kbps", 1000001, false},
		{"kbps", 0, false},
		{"pps", -1, false},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.unit, tc.rate), func(t *testing.T) {
			err := (policy{unit: tc.unit, limits: map[string]int64{"broadcast": tc.rate}}).validate()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
