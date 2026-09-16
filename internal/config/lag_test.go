package config

import (
	"os"
	"path/filepath"
	"reflect"
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
			expected := []LAG{{ID: 1, Name: "uplink", Mode: "dynamic", Members: []string{"ethernet 1/2/1", "ethernet 1/2/2"}}}
			if want {
				expected = append(expected, LAG{ID: 11, Name: "TOFU-STP", Mode: "static", Members: []string{"ethernet 1/1/10", "ethernet 1/1/9"}})
			}
			lags, err := document.LAGs([]string{"ethernet 1/1/9", "ethernet 1/1/10", "ethernet 1/2/1", "ethernet 1/2/2"})
			if err != nil || !reflect.DeepEqual(lags, expected) {
				t.Fatalf("native LAGs=%#v err=%v; want %#v", lags, err, expected)
			}
			got, err = document.HasLAG(1)
			if err != nil || !got {
				t.Fatalf("neighbor LAG missing: present=%v error=%v", got, err)
			}
		})
	}
}

func TestLAGs(t *testing.T) {
	ports := []string{"ethernet 1/1/9", "ethernet 1/1/10", "ethernet 1/2/1"}
	for _, tc := range []struct {
		name, body string
		want       []LAG
		invalid    bool
	}{
		{name: "empty", want: []LAG{}},
		{name: "empty aggregate", body: "lag backup static id 53", want: []LAG{{ID: 53, Name: "backup", Mode: "static", Members: []string{}}}},
		{name: "spaces in name", body: "lag storage name dynamic id 11", want: []LAG{{ID: 11, Name: "storage name", Mode: "dynamic", Members: []string{}}}},
		{name: "syntax in name", body: "lag test static id 9 dynamic id 11", want: []LAG{{ID: 11, Name: "test static id 9", Mode: "dynamic", Members: []string{}}}},
		{name: "members", body: "lag storage dynamic id 11\n ports ethe 1/1/9 to 1/1/10 ethernet 1/2/1\n primary-port 1/1/9", want: []LAG{{ID: 11, Name: "storage", Mode: "dynamic", Members: []string{"ethernet 1/1/10", "ethernet 1/1/9", "ethernet 1/2/1"}}}},
		{name: "overlap", body: "lag storage static id 11\n ports ethe 1/1/9 to 1/1/10 ethe 1/1/9", invalid: true},
		{name: "shared member", body: "lag first static id 11\n ports ethe 1/1/9\nlag second static id 12\n ports ethe 1/1/9", invalid: true},
		{name: "repeated ports", body: "lag storage static id 11\n ports ethe 1/1/9\n ports ethe 1/1/10", invalid: true},
		{name: "missing member", body: "lag storage static id 11\n ports ethe 1/1/9 to 1/1/11", invalid: true},
		{name: "cross-slot range", body: "lag storage static id 11\n ports ethe 1/1/9 to 1/2/1", invalid: true},
		{name: "malformed members", body: "lag storage static id 11\n ports ethe 1/1/9 to", invalid: true},
		{name: "duplicate name", body: "lag same static id 11\nlag same dynamic id 12", invalid: true},
		{name: "duplicate identity", body: "lag first static id 11\nlag second dynamic id 11", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := Parse("ver 09.0.10k\n" + tc.body + "\nend")
			if err != nil {
				t.Fatal(err)
			}
			got, err := document.LAGs(ports)
			if (err != nil) != tc.invalid {
				t.Fatalf("LAGs=%v error=%v", got, err)
			}
			if !tc.invalid && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("LAGs=%#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestLAGRemoval(t *testing.T) {
	lag := LAG{ID: 53, Name: "test", Mode: "dynamic", Members: []string{"ethernet 1/1/7", "ethernet 1/1/8"}}
	base := "ver 09.0.10k\nlag test dynamic id 53\n ports ethe 1/1/7 to 1/1/8\n"
	for _, tc := range []struct {
		name, config string
		blocked      bool
	}{
		{"membership", base + "!\nend", false},
		{"member settings", base + " disable ethe 1/1/7\n port-name member ethernet 1/1/7\n!\nend", false},
		{"disabled range", base + " disable ethe 1/1/7 to 1/1/8\nend", false},
		{"disabled nonmember", base + " disable ethe 1/1/7 to 1/1/9\nend", true},
		{"reversed disable", base + " disable ethe 1/1/8 to 1/1/7\nend", true},
		{"malformed disable", base + " disable ethe 1/1/7 to\nend", true},
		{"aggregate disable", base + " disable\nend", true},
		{"multiword member name", base + " port-name floor east ethernet 1/1/8\nend", false},
		{"named nonmember", base + " port-name stranger ethernet 1/1/9\nend", true},
		{"malformed member name", base + " port-name floor ethernet 1/1/8 extra\nend", true},
		{"overlapping membership", "ver 09.0.10k\nlag test dynamic id 53\n ports ethe 1/1/7 to 1/1/8 ethe 1/1/7\nend", true},
		{"repeated membership", base + " ports ethe 1/1/7\nend", true},
		{"aggregate setting", base + " trunk-threshold 1\n!\nend", true},
		{"virtual interface", base + "!\nvlan 53 by port\n!\ninterface lag 53\n ip address 192.0.2.1 255.255.255.0\n!\nend", true},
		{"unrelated interface", base + "!\ninterface lag 54\n ip address 192.0.2.2 255.255.255.0\n!\nend", false},
		{"empty virtual interface", base + "!\ninterface lag 53\n!\nend", false},
		{"missing aggregate", "ver 09.0.10k\ninterface lag 53\n!\nend", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := Parse(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			if err := document.CheckLAGRemoval(lag.ID, lag.Members); (err != nil) != tc.blocked {
				t.Fatalf("deletion guard: %v; want blocked=%v", err, tc.blocked)
			}
		})
	}
}

func TestLAGRemovalCapture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "lag", "removal-disabled.conf"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := document.CheckLAGRemoval(11, []string{"ethernet 1/1/9", "ethernet 1/1/10"}); err != nil {
		t.Fatal(err)
	}
}
