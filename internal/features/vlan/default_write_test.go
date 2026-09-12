package vlan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestDefaultLifecycle(t *testing.T) {
	var mu sync.Mutex
	current := int64(1)
	inventory := map[int64]string{1: "DEFAULT-VLAN", 53: "INFRA"}
	writes := 0
	failCleanup := false
	server := testswitch.New(t, func(command string) string {
		mu.Lock()
		defer mu.Unlock()
		switch command {
		case "skip-page-display":
			return ""
		case "show version":
			return "SW: Version 09.0.10kT213"
		case "show running-config":
			return fmt.Sprintf("ver 09.0.10kT213\ndefault-vlan-id %d\nvlan 53 name INFRA by port\n tagged ethe 1/1/1\nvlan %d name DEFAULT-VLAN by port\n spanning-tree\nrouter bgp\n local-as 65011\nend", current, current)
		default:
			t.Errorf("unexpected command %q", command)
			return "% Invalid input"
		}
	})
	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			entries := []map[string]any{}
			for id, name := range inventory {
				entries = append(entries, map[string]any{"vlan-id": id, "config": map[string]any{"vlan-id": id, "name": name}})
			}
			json.NewEncoder(w).Encode(map[string]any{"openconfig-network-instance:vlans": map[string]any{"vlan": entries}})
			return
		}
		writes++
		switch r.Method {
		case http.MethodDelete:
			id, err := strconv.ParseInt(r.URL.Path[strings.LastIndex(r.URL.Path, "=")+1:], 10, 64)
			if err != nil || id == current || id == 53 {
				t.Errorf("unsafe deletion %q", r.URL.Path)
				w.WriteHeader(400)
				return
			}
			if failCleanup {
				failCleanup = false
				w.WriteHeader(500)
				return
			}
			delete(inventory, id)
		case http.MethodPatch:
			var body struct {
				VLANs struct {
					VLAN []vlanEntry `json:"vlan"`
				} `json:"vlans"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.VLANs.VLAN) != 1 {
				t.Errorf("invalid selection body: %v", err)
				w.WriteHeader(400)
				return
			}
			entry := body.VLANs.VLAN[0]
			if entry.Config.Name != "DEFAULT-VLAN" || entry.ID != entry.Config.ID {
				t.Error("invalid default selection")
				w.WriteHeader(400)
				return
			}
			// Firmware accepts an identical stale entry without executing the native change.
			if _, exists := inventory[entry.ID]; !exists {
				current = entry.ID
				inventory[current] = "DEFAULT-VLAN"
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(204)
	})
	device, err := fastiron.New(fastiron.Config{
		Host: "switch", Transport: "restconf", Persistence: "manual",
		RESTCONF: &restconf.Config{URL: server.REST.URL, InsecureSkipVerify: true, Timeout: time.Second},
		SSH:      &ssh.Config{Address: server.SSHAddress, Username: "test", Password: "test", KnownHosts: server.KnownHosts, Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if observed, err := applyDefault(ctx, device, 53); err == nil || observed != nil {
		t.Fatalf("occupied target: observed=%v error=%v", observed, err)
	}
	mu.Lock()
	if writes != 0 {
		t.Error("occupied target caused writes")
	}
	inventory[3966] = "DEFAULT-VLAN"
	mu.Unlock()

	for _, desired := range []int64{3966, 3967, 1, 1} {
		observed, err := applyDefault(ctx, device, desired)
		if err != nil || observed == nil || *observed != desired {
			t.Fatalf("select %d: observed=%v error=%v", desired, observed, err)
		}
		mu.Lock()
		if current != desired || len(inventory) != 2 || inventory[53] != "INFRA" {
			t.Errorf("selection %d left native=%d inventory=%v", desired, current, inventory)
		}
		mu.Unlock()
	}

	mu.Lock()
	failCleanup = true
	mu.Unlock()
	observed, err := applyDefault(ctx, device, 4095)
	if err == nil || observed == nil || *observed != 4095 {
		t.Fatalf("cleanup failure lost new identity: observed=%v error=%v", observed, err)
	}
	observed, err = applyDefault(ctx, device, 4095)
	if err != nil || observed == nil || *observed != 4095 {
		t.Fatalf("retry: observed=%v error=%v", observed, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if current != 4095 || len(inventory) != 2 || inventory[53] != "INFRA" {
		t.Fatalf("retry left native=%d inventory=%v", current, inventory)
	}
}

func TestDefaultProperties(t *testing.T) {
	before, err := nativeDefault("ver 09.0.10k\nvlan 1 name DEFAULT-VLAN by port\n spanning-tree\nvlan 53 name INFRA by port\n tagged ethe 1/1/1\nend")
	if err != nil {
		t.Fatal(err)
	}
	moved := "ver 09.0.10k\ndefault-vlan-id 3966\nvlan 53 name INFRA by port\n tagged ethe 1/1/1\nvlan 3966 name DEFAULT-VLAN by port\n spanning-tree\nend"
	for _, tc := range []struct {
		name, configuration string
		preserved           bool
	}{
		{"renumbered", moved, true},
		{"default child removed", strings.Replace(moved, " spanning-tree\n", "", 1), false},
		{"other VLAN changed", strings.Replace(moved, "1/1/1", "1/1/2", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after, err := nativeDefault(tc.configuration)
			if err != nil {
				t.Fatal(err)
			}
			if after.preserves(before) != tc.preserved {
				t.Fatalf("preserved=%v; want %v", after.preserves(before), tc.preserved)
			}
		})
	}
}
