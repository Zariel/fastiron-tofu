package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type (
	stpSetting struct {
		mode     string
		priority int64
	}
	stpSwitch struct {
		running, startup         map[int64]stpSetting
		extra, failClassicDelete bool
		hiddenReads              int
	}
)

func stpConfiguration(base string, vlans map[int64]stpSetting, extra bool) string {
	for _, id := range slices.Sorted(maps.Keys(vlans)) {
		v := vlans[id]
		var b strings.Builder
		if v.mode == "rstp" {
			b.WriteString(" spanning-tree 802-1w\n")
			if v.priority != 32768 {
				fmt.Fprintf(&b, " spanning-tree 802-1w priority %d\n", v.priority)
			}
		} else if v.priority != 32768 {
			fmt.Fprintf(&b, " spanning-tree priority %d\n", v.priority)
		} else {
			b.WriteString(" spanning-tree\n")
		}
		if id == 53 && extra {
			b.WriteString(" spanning-tree 802-1w hello-time 3\n")
		}
		prefix := "vlan " + strconv.FormatInt(id, 10)
		header := ""
		for _, line := range strings.Split(base, "\n") {
			if line == prefix || strings.HasPrefix(line, prefix+" ") {
				header = line + "\n"
				break
			}
		}
		if header != "" {
			base = strings.Replace(base, header, header+b.String(), 1)
		} else {
			base = strings.TrimSuffix(base, "end") + prefix + " by port\n" + b.String() + "!\nend"
		}
	}
	return base
}

func (s *stpSwitch) rest(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && r.URL.Path == "/restconf/data/stp/interfaces" {
		fmt.Fprint(w, `{"openconfig-spanning-tree:interfaces":{"interface":[{"name":"ethernet 1/1/12","config":{"name":"ethernet 1/1/12","bpdu-guard":true,"guard":"ROOT"}}]}}`)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/restconf/data/stp" {
		classic, rapid := []any{}, []any{}
		for id, v := range s.running {
			if id == 53 && s.hiddenReads > 0 {
				s.hiddenReads--
				continue
			}
			if v.mode == "rstp" {
				rapid = append(rapid, map[string]any{"vlan-id": id, "config": map[string]any{"vlan-id": id, "bridge-priority": v.priority}})
			} else {
				classic = append(classic, map[string]any{"vlan-id": id, "config": map[string]any{"vlan-id": id, "pvst-priority": v.priority}})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"openconfig-spanning-tree:stp": map[string]any{"rapid-pvst": map[string]any{"vlan": rapid}, "icx-openconfig-spanning-tree-aug:pvst": map[string]any{"vlan": classic}}})
		return
	}
	endpoint := r.URL.Path
	mode, container, priority := "stp", "pvst", "pvst-priority"
	collection := "/restconf/data/stp/icx-openconfig-spanning-tree-aug:pvst"
	if strings.HasPrefix(endpoint, "/restconf/data/stp/rapid-pvst") {
		mode, container, priority, collection = "rstp", "rapid-pvst", "bridge-priority", "/restconf/data/stp/rapid-pvst"
	}
	if r.Method == "DELETE" {
		value, ok := strings.CutPrefix(endpoint, collection+"/vlan=")
		id, err := strconv.ParseInt(value, 10, 64)
		if !ok || err != nil || s.running[id].mode != mode {
			http.NotFound(w, r)
			return
		}
		if mode == "rstp" {
			s.running[id] = stpSetting{mode: "stp", priority: 32768}
			s.hiddenReads = 1
		} else {
			if s.failClassicDelete {
				http.Error(w, "cannot remove classic entry", 500)
				return
			}
			delete(s.running, id)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if endpoint != collection || r.Method != "POST" && r.Method != "PATCH" {
		http.NotFound(w, r)
		return
	}
	var raw map[string]json.RawMessage
	if json.NewDecoder(r.Body).Decode(&raw) != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	if r.Method == "PATCH" {
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw[container], &nested) != nil {
			http.Error(w, "missing container", 400)
			return
		}
		raw = nested
	}
	var vlans []struct {
		ID     int64            `json:"vlan-id"`
		Config map[string]int64 `json:"config"`
	}
	if json.Unmarshal(raw["vlan"], &vlans) != nil {
		http.Error(w, "missing VLAN list", 400)
		return
	}
	for _, v := range vlans {
		_, exists := s.running[v.ID]
		if r.Method == "POST" && exists {
			http.Error(w, "entry exists", 409)
			return
		}
		if r.Method == "PATCH" && (!exists || s.running[v.ID].mode != mode) {
			http.NotFound(w, r)
			return
		}
		s.running[v.ID] = stpSetting{mode: mode, priority: v.Config[priority]}
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestOpenTofuSTP(t *testing.T) {
	s := newSwitch(t)
	s.running[53], s.startup[53] = "TEST", "TEST"
	s.stp = &stpSwitch{running: map[int64]stpSetting{1: {mode: "stp", priority: 32768}}, startup: map[int64]stpSetting{1: {mode: "stp", priority: 32768}}}
	write, run, base := tofuFixture(t, s)
	config := func(mode string, priority int) {
		write("main.tf", base+fmt.Sprintf(`resource "fastiron_spanning_tree_vlan" "test" {
 vlan_id = 53
 mode = %q
 priority = %d
}
data "fastiron_spanning_tree" "test" { depends_on = [fastiron_spanning_tree_vlan.test] }
output "vlans" { value = data.fastiron_spanning_tree.test.vlans }
output "interfaces" { value = data.fastiron_spanning_tree.test.interfaces }
`, mode, priority))
	}
	check := func(want map[int64]stpSetting) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !maps.Equal(s.stp.running, want) || !maps.Equal(s.stp.startup, want) {
			t.Fatalf("STP running=%v startup=%v want=%v", s.stp.running, s.stp.startup, want)
		}
		if s.running[53] != "TEST" || s.startup[53] != "TEST" {
			t.Fatal("spanning-tree operation changed VLAN existence or name")
		}
	}
	config("rstp", 12345)
	run(0, "init", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[int64]stpSetting{1: {mode: "stp", priority: 32768}, 53: {mode: "rstp", priority: 12345}})
	if output := strings.TrimSpace(run(0, "output", "-json", "vlans")); output != `{"1":{"mode":"stp","priority":32768},"53":{"mode":"rstp","priority":12345}}` {
		t.Fatalf("STP discovery=%s", output)
	}
	if output := strings.TrimSpace(run(0, "output", "-json", "interfaces")); output != `{"ethernet 1/1/12":{"admin_edge":false,"bpdu_guard":true,"root_guard":true}}` {
		t.Fatalf("STP interface discovery=%s", output)
	}
	run(0, "state", "rm", "fastiron_spanning_tree_vlan.test")
	run(0, "import", "-no-color", "fastiron_spanning_tree_vlan.test", "53")
	run(0, "plan", "-detailed-exitcode", "-no-color")

	config("rstp", 23456)
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[int64]stpSetting{1: {mode: "stp", priority: 32768}, 53: {mode: "rstp", priority: 23456}})
	config("stp", 20000)
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[int64]stpSetting{1: {mode: "stp", priority: 32768}, 53: {mode: "stp", priority: 20000}})
	config("rstp", 0)
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[int64]stpSetting{1: {mode: "stp", priority: 32768}, 53: {mode: "rstp", priority: 0}})
	s.mu.Lock()
	s.stp.running[53] = stpSetting{mode: "rstp", priority: 4444}
	s.mu.Unlock()
	run(2, "plan", "-detailed-exitcode", "-no-color")
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[int64]stpSetting{1: {mode: "stp", priority: 32768}, 53: {mode: "rstp", priority: 0}})

	write("main.tf", base)
	s.mu.Lock()
	s.stp.extra = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	check(map[int64]stpSetting{1: {mode: "stp", priority: 32768}, 53: {mode: "rstp", priority: 0}})
	s.mu.Lock()
	s.stp.extra = false
	s.stp.failClassicDelete = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	if got := s.stp.running[53]; got != (stpSetting{mode: "stp", priority: 32768}) {
		t.Errorf("RSTP removal fallback=%v", got)
	}
	s.stp.failClassicDelete = false
	s.failSave = true
	s.mu.Unlock()
	run(1, "apply", "-auto-approve", "-no-color")
	s.mu.Lock()
	s.failSave = false
	s.mu.Unlock()
	run(0, "apply", "-auto-approve", "-no-color")
	check(map[int64]stpSetting{1: {mode: "stp", priority: 32768}})
	run(0, "plan", "-detailed-exitcode", "-no-color")
}
