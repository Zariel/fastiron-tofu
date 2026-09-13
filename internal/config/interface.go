package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

//go:generate ragel -Z -o command_parser.go command.rl
//go:generate gofumpt -w command_parser.go

type kind uint8

const (
	unknown kind = iota
	trustDSCP
	protectedPort
	voiceVLAN
	stormLimit
)

// InterfacePolicy is the native policy selected by one resource's ownership.
// Remaining preserves every other command, including policies on other ports.
type InterfacePolicy struct {
	Enabled   bool
	VLAN      int64
	Unit      string
	Limits    map[string]int64
	Options   bool
	Remaining []string
}

// Policy identifies the independently owned interface configuration to extract.
type Policy uint8

const (
	DSCPTrust Policy = iota
	Protection
	VoiceVLAN
	StormControl
)

func (d *Document) InterfacePolicy(name string, policy Policy) (InterfacePolicy, error) {
	header, err := d.Interface(name)
	if err != nil {
		return InterfacePolicy{}, err
	}
	state := InterfacePolicy{Limits: map[string]int64{}}
	wanted := [...]kind{trustDSCP, protectedPort, voiceVLAN, stormLimit}
	if int(policy) >= len(wanted) {
		return state, errors.New("unknown interface policy")
	}
	for i, c := range d.Commands {
		// The stanza itself may appear or disappear with its only owned setting.
		if i == header {
			continue
		}
		if header < 0 || c.Parent != header || commandKind(strings.TrimSpace(c.Text)) != wanted[policy] {
			state.Remaining = append(state.Remaining, c.Text)
			continue
		}
		if err := state.consume(c.Fields, wanted[policy]); err != nil {
			return InterfacePolicy{}, err
		}
	}
	return state, nil
}

func (s *InterfacePolicy) consume(fields []string, k kind) error {
	switch k {
	case trustDSCP, protectedPort:
		count := 1
		if k == trustDSCP {
			count = 2
		}
		if len(fields) != count || s.Enabled {
			return errors.New("native interface flag is malformed or repeated")
		}
		s.Enabled = true
	case voiceVLAN:
		if len(fields) != 2 || s.VLAN != 0 {
			return errors.New("native interface voice VLAN is ambiguous")
		}
		id, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || id < 1 || id > 4095 || fields[1] != strconv.FormatInt(id, 10) {
			return errors.New("native interface voice VLAN is invalid")
		}
		s.VLAN = id
	case stormLimit:
		if len(fields) < 3 {
			return errors.New("native storm limit is missing its rate")
		}
		rate, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || rate <= 0 {
			return errors.New("native storm limit is invalid")
		}
		if _, duplicate := s.Limits[fields[0]]; duplicate {
			return errors.New("native configuration repeats a storm traffic class")
		}
		unit, options := "pps", fields[3:]
		if len(options) > 0 && (options[0] == "kbps" || options[0] == "pps") {
			unit, options = options[0], options[1:]
		}
		if len(options) > 0 && options[0] != "log" && options[0] != "threshold" {
			return fmt.Errorf("unsupported native storm option %q", options[0])
		}
		if s.Unit != "" && s.Unit != unit {
			return errors.New("native storm limits use different units on one interface")
		}
		s.Unit = unit
		s.Limits[fields[0]] = rate
		s.Options = s.Options || len(options) != 0
	}
	return nil
}

func (d *Document) SymmetricFlowControl() bool {
	for _, c := range d.Commands {
		if c.Parent == -1 && len(c.Fields) > 1 && c.Fields[0] == "symmetrical-flow-control" {
			return true
		}
	}
	return false
}
