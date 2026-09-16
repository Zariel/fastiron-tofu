package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLAGInterface(t *testing.T) {
	for _, tc := range []struct {
		file string
		id   int64
		want LAGInterface
	}{
		{"interface-description.conf", 31, LAGInterface{PortName: "TOFU-REST-DESCRIPTION", Enabled: true}},
		{"interface-disabled.conf", 31, LAGInterface{Enabled: false}},
		{"interface-members.conf", 32, LAGInterface{Enabled: true}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "lag", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			document, err := Parse(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := document.LAGInterface(tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("interface=%+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLAGInterfaceScope(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		invalid    bool
	}{
		{"empty parent", "lag EMPTY static id 5\n", false},
		{"other interface", "lag TEST static id 5\ninterface lag 6\n port-name OTHER\n disable\n", false},
		{"nested settings", "lag TEST static id 5\ninterface lag 5\n protocol future\n  port-name NESTED\n  disable\n", false},
		{"banner", "lag TEST static id 5\nbanner motd #\ninterface lag 5\n port-name FAKE\n disable\n#\n", false},
		{"missing parent", "interface lag 5\n port-name STALE\n", true},
		{"duplicate parent", "lag ONE static id 5\nlag TWO static id 5\n", true},
		{"duplicate interface", "lag TEST static id 5\ninterface lag 5\ninterface lag 5\n", true},
		{"duplicate name", "lag TEST static id 5\ninterface lag 5\n port-name ONE\n port-name TWO\n", true},
		{"missing name", "lag TEST static id 5\ninterface lag 5\n port-name\n", true},
		{"duplicate disable", "lag TEST static id 5\ninterface lag 5\n disable\n disable\n", true},
		{"member disable in interface", "lag TEST static id 5\ninterface lag 5\n disable ethe 1/1/9\n", true},
		{"malformed disable", "lag TEST static id 5\ninterface lag 5\n disable extra\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := Parse("ver 09.0.10kT213\n" + tc.body + "end\n")
			if err != nil {
				t.Fatal(err)
			}
			got, err := document.LAGInterface(5)
			if (err != nil) != tc.invalid {
				t.Fatalf("interface=%+v, error=%v", got, err)
			}
			if !tc.invalid && got != (LAGInterface{Enabled: true}) {
				t.Fatalf("unrelated settings changed defaults: %+v", got)
			}
		})
	}
}
