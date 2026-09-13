package config

import (
	"errors"
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
	header, err := d.interfaceHeader(name)
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
		if header < 0 || c.Parent != header || c.kind != wanted[policy] {
			state.Remaining = append(state.Remaining, c.Text)
			continue
		}
		if err := state.consume(c); err != nil {
			return InterfacePolicy{}, err
		}
	}
	return state, nil
}

func (s *InterfacePolicy) consume(c Command) error {
	if !c.valid {
		return errors.New("native interface policy is malformed")
	}
	switch c.kind {
	case trustDSCP, protectedPort:
		if s.Enabled {
			return errors.New("native interface flag is malformed or repeated")
		}
		s.Enabled = true
	case voiceVLAN:
		if s.VLAN != 0 {
			return errors.New("native interface voice VLAN is ambiguous")
		}
		id := c.number
		if id < 1 || id > 4095 {
			return errors.New("native interface voice VLAN is invalid")
		}
		s.VLAN = id
	case stormLimit:
		rate := c.number
		if rate <= 0 {
			return errors.New("native storm limit is invalid")
		}
		if _, duplicate := s.Limits[c.name]; duplicate {
			return errors.New("native configuration repeats a storm traffic class")
		}
		unit := c.unit
		if unit == "" {
			unit = "pps"
		}
		if s.Unit != "" && s.Unit != unit {
			return errors.New("native storm limits use different units on one interface")
		}
		s.Unit = unit
		s.Limits[c.name] = rate
		s.Options = s.Options || c.options
	}
	return nil
}

func (d *Document) SymmetricFlowControl() bool {
	for _, c := range d.Commands {
		if c.Parent == -1 && c.kind == symmetricFlowControl {
			return true
		}
	}
	return false
}
