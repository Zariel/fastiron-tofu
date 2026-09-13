package lldp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMEDCacheCaptures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "testdata", "lldp-med", "*.rest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no MED cache captures")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				MED json.RawMessage `json:"icx-openconfig-lldp-aug:med"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}
			if _, err := decodeMEDCache(response.MED); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMEDCachePaths(t *testing.T) {
	for _, tc := range []struct {
		file  string
		paths []string
	}{
		{"write-stale-before-delete.rest.json", []string{
			"/lldp/med/network-policy=voice,untagged/untagged=24/ports=ethernet%201%2F1%2F11",
			"/lldp/med/network-policy=voice,tagged/tagged=3053,3,46/ports=ethernet%201%2F1%2F11",
		}},
		{"priority-tagged-after-port.rest.json", []string{
			"/lldp/med/network-policy=voice,priority-tagged/priority-tagged=5,40/ports=ethernet%201%2F1%2F10",
			"/lldp/med/network-policy=voice,priority-tagged/priority-tagged=5,40/ports=ethernet%201%2F1%2F12",
		}},
		{"write-stale-after-delete.rest.json", []string{
			"/lldp/med/network-policy=voice,untagged/untagged=24/ports=ethernet%201%2F1%2F11",
		}},
		{"defaults.rest.json", nil},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "config", "testdata", "lldp-med", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				MED json.RawMessage `json:"icx-openconfig-lldp-aug:med"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}
			attachments, err := decodeMEDCache(response.MED)
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, a := range attachments {
				paths = append(paths, a.path())
			}
			slices.Sort(paths)
			slices.Sort(tc.paths)
			if !slices.Equal(paths, tc.paths) {
				t.Fatalf("paths=%v; want %v", paths, tc.paths)
			}
		})
	}
}

func TestMEDCacheRejectsIncompletePolicies(t *testing.T) {
	for _, entry := range []string{
		`{"application":"voice","traffic":"tagged","tagged":[{"priority":3,"dscp":46,"ports":["ethernet 1/1/11"]}]}`,
		`{"application":"voice","traffic":"priority-tagged","priority-tagged":[{"dscp":46,"ports":["ethernet 1/1/11"]}]}`,
		`{"application":"voice","traffic":"untagged","untagged":[{"ports":["ethernet 1/1/11"]}]}`,
		`{"application":"voice","traffic":"untagged","untagged":[{"dscp":64,"ports":["ethernet 1/1/11"]}]}`,
		`{"application":"voice","traffic":"untagged","untagged":[{"dscp":0,"ports":["ethernet 1/1/11","ethernet 1/1/11"]}]}`,
		`{"application":"voice","traffic":"untagged","untagged":[{"dscp":0,"ports":["lag 1"]}]}`,
		`{"application":"unknown","traffic":"untagged","untagged":[{"dscp":0,"ports":["ethernet 1/1/11"]}]}`,
		`{"application":"voice","traffic":"tagged","untagged":[{"dscp":0,"ports":["ethernet 1/1/11"]}]}`,
	} {
		if _, err := decodeMEDCache(json.RawMessage(`{"network-policy":[` + entry + `]}`)); err == nil {
			t.Fatalf("accepted incomplete cache entry: %s", entry)
		}
	}
}
