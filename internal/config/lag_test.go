package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLAGPresence(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		present, invalid bool
	}{
		{name: "static", body: "lag test static id 11", present: true},
		{name: "dynamic", body: "lag test dynamic id 11", present: true},
		{name: "name with spaces", body: "lag test name static id 11", present: true},
		{name: "name with syntax", body: "lag test static id 9 dynamic id 11", present: true},
		{name: "numeric name", body: "lag test static id 9223372036854775808 dynamic id 11", present: true},
		{name: "other aggregate", body: "lag test static id 1"},
		{name: "policy is not parent", body: "interface lag 11\n stp-bpdu-guard"},
		{name: "nested header", body: "vlan 11\n lag test static id 11"},
		{name: "duplicate", body: "lag test static id 11\nlag other dynamic id 11", invalid: true},
		{name: "missing name", body: "lag   static id 11", invalid: true},
		{name: "malformed", body: "lag test static id", invalid: true},
		{name: "unsupported mode", body: "lag test unknown id 11", invalid: true},
		{name: "zero", body: "lag test static id 0", invalid: true},
		{name: "overflow", body: "lag test static id 9223372036854775808", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := Parse("ver 09.0.10k\n" + tc.body + "\nend")
			if err != nil {
				t.Fatal(err)
			}
			got, err := document.HasLAG(11)
			if (err != nil) != tc.invalid || got != tc.present {
				t.Fatalf("present=%v err=%v", got, err)
			}
		})
	}
}

func TestLAGCaptures(t *testing.T) {
	for name, want := range map[string]bool{"lag-empty": true, "lag-all": true, "lag-detached": false} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "stp-interfaces", name+".conf"))
			if err != nil {
				t.Fatal(err)
			}
			document, err := Parse(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := document.HasLAG(11)
			if err != nil || got != want {
				t.Fatalf("LAG 11 present=%v error=%v; want %v", got, err, want)
			}
			got, err = document.HasLAG(1)
			if err != nil || !got {
				t.Fatalf("neighbor LAG missing: present=%v error=%v", got, err)
			}
		})
	}
}
