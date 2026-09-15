package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPoECaptures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "poe", "*.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no PoE captures")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(strings.TrimSuffix(file, ".conf") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				Interface string
				Policy    PoEPolicy
			}
			if err := json.Unmarshal(data, &expected); err != nil {
				t.Fatal(err)
			}
			d, err := Parse(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := d.PoE(expected.Interface)
			if err != nil || got != expected.Policy {
				t.Fatalf("policy=%+v error=%v, want %+v", got, err, expected.Policy)
			}
		})
	}
}

func TestPoEPolicy(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    PoEPolicy
	}{
		{"inline power", PoEPolicy{Enabled: true, Priority: 3}},
		{"no inline power", PoEPolicy{Priority: 3}},
		{"inline power priority 1 power-by-class 4", PoEPolicy{Enabled: true, Priority: 1, PowerByClass: 4}},
		{"inline power power-limit 95000 priority 2", PoEPolicy{Enabled: true, Priority: 2, PowerLimitMilliwatts: 95000}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			for _, global := range []bool{false, true} {
				command := "interface ethernet 1/1/12\n " + tc.command
				if global {
					command = strings.Replace(tc.command, "inline power", "inline power ethernet 1/1/12", 1)
				}
				d, err := Parse("ver 09.0.10k\n" + command + "\nend")
				if err != nil {
					t.Fatal(err)
				}
				got, remaining, err := d.PoE("ethernet 1/1/12")
				if err != nil || got != tc.want || !slices.Equal(remaining, []string{"ver 09.0.10k", "end"}) {
					t.Fatalf("global=%t policy=%+v remaining=%v error=%v", global, got, remaining, err)
				}
			}
		})
	}
}

func TestPoEOwnership(t *testing.T) {
	raw := "ver 09.0.10k\ninline power overdrive\ninline power ethernet 1/1/11 priority 1\ninline power ethernet 1/1/11 overdrive\ninterface ethernet 1/1/12\n port-name PHONE\n disable\n inline power priority 2 power-limit 12000\ninterface ethernet 1/1/10\n no inline power\nend"
	d, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, remaining, err := d.PoE("ethernet 1/1/12")
	want := PoEPolicy{Enabled: true, Priority: 2, PowerLimitMilliwatts: 12000}
	if err != nil || got != want {
		t.Fatalf("policy=%+v error=%v", got, err)
	}
	expected := []string{"ver 09.0.10k", "inline power overdrive", "inline power ethernet 1/1/11 priority 1", "inline power ethernet 1/1/11 overdrive", " port-name PHONE", " disable", "interface ethernet 1/1/10", " no inline power", "end"}
	if !slices.Equal(remaining, expected) {
		t.Fatalf("unowned commands changed: %v", remaining)
	}
	got, _, err = d.PoE("ethernet 1/1/9")
	if err != nil || got != (PoEPolicy{Enabled: true, Priority: 3}) {
		t.Fatalf("default policy=%+v error=%v", got, err)
	}
}

func TestPoEInvalid(t *testing.T) {
	for _, command := range []string{
		"inline power priority", "inline power priority 0", "inline power priority 4",
		"inline power power-by-class 5", "inline power power-by-class -1",
		"inline power power-limit 999", "inline power power-limit 95001",
		"inline power power-limit 999999999999999999999999999999",
		"inline power power-limit 12000 power-by-class 0",
		"inline power priority 1 priority 2", "inline power priority 1 trailing",
		"no inline power priority 1", "inline power overdrive",
		"inline power\n no inline power", "inline power ethernet 1/1/12 priority 2",
	} {
		t.Run(command, func(t *testing.T) {
			d, err := Parse("ver 09.0.10k\ninterface ethernet 1/1/12\n " + command + "\nend")
			if err != nil {
				t.Fatal(err)
			}
			if got, _, err := d.PoE("ethernet 1/1/12"); err == nil {
				t.Fatalf("accepted malformed policy: %+v", got)
			}
		})
	}
	for _, command := range []string{
		"inline power ethernet", "inline power ethernet 1/1",
		"inline power ethernet 1/1/12 priority", "inline power ethernet 0/1/12 priority 2",
		"inline power ethernet 1/1/12 priority 2\ninterface ethernet 1/1/12\n no inline power",
	} {
		d, err := Parse("ver 09.0.10k\n" + command + "\nend")
		if err != nil {
			t.Fatal(err)
		}
		if got, _, err := d.PoE("ethernet 1/1/12"); err == nil {
			t.Fatalf("accepted %q: %+v", command, got)
		}
	}
}
