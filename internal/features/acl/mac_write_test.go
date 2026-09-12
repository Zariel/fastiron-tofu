package acl

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type macSwitch struct {
	standardSwitch
	device         *fastiron.Device
	entries        map[int64]macRESTEntry
	present        bool
	failAfterWrite bool
	omitRESTLog    bool
	deleted        []int64
}

func newMACSwitch(t *testing.T, present bool, entries ...macRESTEntry) *macSwitch {
	t.Helper()
	s := &macSwitch{present: present, entries: map[int64]macRESTEntry{}}
	for _, entry := range entries {
		s.entries[entry.ID] = entry
	}
	s.publish()
	s.device = s.standardSwitch.device(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			var body struct {
				Sets struct {
					ACLs []struct {
						Entries struct {
							Rules []macRESTEntry `json:"acl-entry"`
						} `json:"acl-entries"`
					} `json:"acl-set"`
				} `json:"acl-sets"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Sets.ACLs) != 1 || len(body.Sets.ACLs[0].Entries.Rules) == 0 {
				w.WriteHeader(400)
				return
			}
			for _, entry := range body.Sets.ACLs[0].Entries.Rules {
				for _, existing := range s.entries {
					if existing.ID == entry.ID || (reflect.DeepEqual(existing.L2, entry.L2) && existing.Actions == entry.Actions) {
						w.WriteHeader(500)
						return
					}
				}
				s.entries[entry.ID] = entry
			}
			s.present = true
		case http.MethodDelete:
			if r.URL.Path == "/restconf/data/acl/acl-sets/acl-set/TEST/ACL_L2" {
				s.present = false
				clear(s.entries)
				break
			}
			id, err := strconv.ParseInt(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], 10, 64)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			delete(s.entries, id)
			s.deleted = append(s.deleted, id)
		default:
			t.Errorf("unexpected MAC mutation: %s", r.Method)
			w.WriteHeader(405)
			return
		}
		s.publish()
		if s.failAfterWrite {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	})
	return s
}

func (s *macSwitch) publish() {
	native := ""
	sets := []any{}
	if s.present {
		native = "mac access-list TEST\n"
		entries := []macRESTEntry{}
		for _, id := range slices.Sorted(maps.Keys(s.entries)) {
			entry := s.entries[id]
			restEntry := entry
			if s.omitRESTLog {
				restEntry.Actions.Config.Log = ""
			}
			entries = append(entries, restEntry)
			action := "permit"
			if strings.TrimPrefix(entry.Actions.Config.Forward, "openconfig-acl:") == "DROP" {
				action = "deny"
			}
			match := entry.L2.Config
			address := func(value, mask string) string {
				if value == "any" || mask == "00:00:00:00:00:00" {
					return "any"
				}
				return value + " " + mask
			}
			native += " " + action + " " + address(match.Source, match.SourceMask) + " " + address(match.Destination, match.DestinationMask)
			if match.EtherType != nil {
				native += fmt.Sprintf(" ether-type %04x", *match.EtherType)
			}
			if strings.TrimPrefix(entry.Actions.Config.Log, "openconfig-acl:") == "LOG_SYSLOG" {
				native += " log"
			}
			native += "\n"
		}
		sets = append(sets, map[string]any{"name": "TEST", "type": "ACL_L2", "acl-entries": map[string]any{"acl-entry": entries}})
	}
	s.running = absentStandard + native + neighbor
	s.restACLs = map[string]any{"openconfig-acl:acl-sets": map[string]any{"acl-set": sets}}
}

func testMACEntry(id int64, action string, etherType int64) macRESTEntry {
	entry := macRESTEntry{ID: id}
	entry.Config.ID = &id
	entry.Actions.Config.Forward = action
	entry.L2.Config.Source, entry.L2.Config.SourceMask = "00:00:00:00:00:00", "00:00:00:00:00:00"
	entry.L2.Config.Destination, entry.L2.Config.DestinationMask = "00:00:00:00:00:00", "00:00:00:00:00:00"
	entry.L2.Config.EtherType = &etherType
	return entry
}

func TestMACReconcile(t *testing.T) {
	s := newMACSwitch(t, true, testMACEntry(37, "ACCEPT", 2048), testMACEntry(89, "DROP", 2054))
	desired := macConfig{Name: "TEST", Rules: []macRule{
		{Action: "permit", EtherType: optionalInt{Value: 34525, Present: true}},
		{Action: "deny", EtherType: optionalInt{Value: 2054, Present: true}},
	}}
	if _, err := applyMAC(context.Background(), s.device, desired); err != nil {
		t.Fatal(err)
	}
	want := absentStandard + "mac access-list TEST\n permit any any ether-type 86dd\n deny any any ether-type 0806\n" + neighbor
	if s.running != want || s.startup != want || len(s.entries) != 2 || slices.Contains(s.deleted, 89) {
		t.Fatal("MAC reconciliation did not preserve order, saved configuration and unchanged entry")
	}
	writes := s.writes
	s.entries = map[int64]macRESTEntry{10: testMACEntry(10, "ACCEPT", 34525), 20: testMACEntry(20, "DROP", 2054)}
	s.publish()
	if _, err := applyMAC(context.Background(), s.device, desired); err != nil || s.writes != writes {
		t.Fatalf("converged MAC ACL was rewritten: %v", err)
	}
}

func TestMACAppend(t *testing.T) {
	for _, id := range []int64{37, 65000} {
		t.Run(strconv.FormatInt(id, 10), func(t *testing.T) {
			s := newMACSwitch(t, true, testMACEntry(id, "ACCEPT", 2048))
			desired := macConfig{Name: "TEST", Rules: []macRule{
				{Action: "permit", EtherType: optionalInt{Value: 2048, Present: true}},
				{Action: "deny"},
			}}
			if _, err := applyMAC(context.Background(), s.device, desired); err != nil {
				t.Fatal(err)
			}
			want := absentStandard + "mac access-list TEST\n permit any any ether-type 0800\n deny any any\n" + neighbor
			if s.running != want || s.startup != want {
				t.Fatal("append did not preserve ordered native and saved rules")
			}
			if id == 37 && len(s.deleted) != 0 {
				t.Fatal("append unnecessarily removed an existing rule")
			}
		})
	}
}

func TestMACEmpty(t *testing.T) {
	s := newMACSwitch(t, false)
	if _, err := applyMAC(context.Background(), s.device, macConfig{Name: "TEST"}); err != nil {
		t.Fatal(err)
	}
	want := absentStandard + "mac access-list TEST\n" + neighbor
	if s.running != want || s.startup != want {
		t.Fatal("empty creation did not remove the temporary rule before saving")
	}
	if err := deleteMAC(context.Background(), s.device, "TEST"); err != nil {
		t.Fatal(err)
	}
	if s.running != absentStandard+neighbor || s.startup != s.running {
		t.Fatal("MAC ACL deletion did not preserve neighboring configuration")
	}
}

func TestMACRetry(t *testing.T) {
	s := newMACSwitch(t, true, testMACEntry(37, "ACCEPT", 2048))
	before := s.startup
	s.failAfterWrite = true
	desired := macConfig{Name: "TEST", Rules: []macRule{{Action: "deny"}}}
	observed, err := applyMAC(context.Background(), s.device, desired)
	if err == nil || observed == nil || len(observed.Rules) != 0 || s.startup != before {
		t.Fatalf("partial deletion lost observed state or saved failed work: observed=%+v err=%v", observed, err)
	}

	s.failAfterWrite = false
	if _, err := applyMAC(context.Background(), s.device, desired); err != nil {
		t.Fatal(err)
	}
	want := absentStandard + "mac access-list TEST\n deny any any\n" + neighbor
	if s.running != want || s.startup != want {
		t.Fatal("retry did not converge and save the MAC ACL")
	}
}

func TestMACSaveRetry(t *testing.T) {
	s := newMACSwitch(t, true, testMACEntry(37, "ACCEPT", 2048))
	s.failSave = true
	if err := deleteMAC(context.Background(), s.device, "TEST"); err == nil {
		t.Fatal("failed save was ignored")
	}
	if s.present || !strings.Contains(s.startup, "mac access-list TEST") {
		t.Fatal("test did not leave an unsaved deletion")
	}

	s.failSave = false
	writes := s.writes
	if err := deleteMAC(context.Background(), s.device, "TEST"); err != nil {
		t.Fatal(err)
	}
	if s.writes != writes || s.startup != absentStandard+neighbor {
		t.Fatal("retry did not save absence without another mutation")
	}
}

func TestMACReference(t *testing.T) {
	s := newMACSwitch(t, true, testMACEntry(37, "ACCEPT", 2048))
	s.running = strings.Replace(s.running, "interface ethernet 1/1/10\n", "interface ethernet 1/1/10\n mac access-group TEST in\n", 1)
	s.startup = s.running
	if err := deleteMAC(context.Background(), s.device, "TEST"); err == nil || !strings.Contains(err.Error(), "remove native ACL references") {
		t.Fatalf("bound deletion: %v", err)
	}
	if s.writes != 0 || s.saves != 0 || s.running != s.startup {
		t.Fatal("referenced MAC ACL was changed")
	}
}

func TestMACReloadLogging(t *testing.T) {
	for name, tc := range map[string]struct {
		logged int
		want   string
	}{
		"logged first": {0, " deny any any ether-type 86dd log\n deny any any ether-type 0800\n"},
		"logged last":  {1, " deny any any ether-type 0800\n deny any any ether-type 86dd log\n"},
	} {
		t.Run(name, func(t *testing.T) {
			entries := []macRESTEntry{testMACEntry(10, "DROP", 2048), testMACEntry(20, "DROP", 2048)}
			entries[tc.logged].Actions.Config.Log = "LOG_SYSLOG"
			s := newMACSwitch(t, true, entries...)
			s.omitRESTLog = true
			s.publish()
			desired := macConfig{Name: "TEST", Rules: []macRule{
				{Action: "deny", EtherType: optionalInt{Value: 2048, Present: true}},
				{Action: "deny", EtherType: optionalInt{Value: 2048, Present: true}},
			}}
			desired.Rules[tc.logged].Log = true
			desired.Rules[tc.logged].EtherType.Value = 34525

			if _, err := applyMAC(context.Background(), s.device, desired); err != nil {
				t.Fatal(err)
			}
			want := absentStandard + "mac access-list TEST\n" + tc.want + neighbor
			if s.running != want || s.startup != want {
				t.Fatalf("logged rule update: running=%q startup=%q; want %q", s.running, s.startup, want)
			}
			writes := s.writes
			if _, err := applyMAC(context.Background(), s.device, desired); err != nil {
				t.Fatal(err)
			}
			if s.writes != writes {
				t.Fatal("converged MAC ACL caused another write")
			}
		})
	}
}
