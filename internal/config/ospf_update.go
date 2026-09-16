package config

import (
	"errors"
	"net/netip"
	"slices"
)

// CheckOSPFAreaUpdate verifies that only the selected area's existence changed.
func (d *Document) CheckOSPFAreaUpdate(before *Document, id string) error {
	wanted, err := ospfID(id)
	if err != nil {
		return err
	}
	previous, hadProcess, err := before.ospfAreaRemaining(wanted)
	if err != nil {
		return err
	}
	current, hasProcess, err := d.ospfAreaRemaining(wanted)
	if err != nil {
		return err
	}
	// Creating the first area may enable OSPF; area deletion does not own the process.
	if hadProcess && !hasProcess || !slices.Equal(previous, current) {
		return errors.New("OSPF area operation changed unrelated native configuration")
	}
	return nil
}

func (d *Document) ospfAreaRemaining(id netip.Addr) ([]string, bool, error) {
	if _, err := d.OSPFAreas(); err != nil {
		return nil, false, err
	}
	process := -1
	var remaining, areas []string
	scopes := make([]string, len(d.Commands))
	for i, c := range d.Commands {
		scopes[i] = c.Text
		if c.Parent >= 0 {
			scopes[i] = scopes[c.Parent] + "\n" + c.Text
		}
		if c.Parent == -1 && c.kind == ospfRouter && c.name == "" {
			process = i
			continue
		}
		if process >= 0 && c.Parent == process && c.kind == ospfArea {
			current, err := ospfID(c.name)
			if err != nil {
				return nil, false, err
			}
			if current != id || c.options {
				areas = append(areas, scopes[i])
			}
			continue
		}
		remaining = append(remaining, scopes[i])
	}
	// Adding an area may reorder the process's area declarations and options.
	slices.Sort(areas)
	return append(remaining, areas...), process >= 0, nil
}

// CheckOSPFBindingUpdate verifies that only the selected interface binding changed.
func (d *Document) CheckOSPFBindingUpdate(before *Document, id, name string) error {
	wanted, err := ospfID(id)
	if err != nil {
		return err
	}
	previous, err := before.ospfBindingRemaining(wanted, name)
	if err != nil {
		return err
	}
	current, err := d.ospfBindingRemaining(wanted, name)
	if err != nil {
		return err
	}
	if !slices.Equal(previous, current) {
		return errors.New("OSPF binding operation changed unrelated native configuration")
	}
	return nil
}

func (d *Document) ospfBindingRemaining(id netip.Addr, name string) ([]string, error) {
	if _, err := d.OSPFAreas(); err != nil {
		return nil, err
	}
	header, err := d.interfaceHeader(name)
	if err != nil {
		return nil, err
	}
	var remaining []string
	scopes := make([]string, len(d.Commands))
	for i, c := range d.Commands {
		scopes[i] = c.Text
		if c.Parent >= 0 {
			scopes[i] = scopes[c.Parent] + "\n" + c.Text
		}
		// An interface containing only the owned binding may lose its empty stanza.
		if i == header {
			continue
		}
		if header >= 0 && c.Parent == header && c.kind == interfaceVRF {
			return nil, errors.New("OSPF binding operation targets an interface in another VRF")
		}
		if header >= 0 && c.Parent == header && c.kind == ospfBinding {
			current, err := ospfID(c.name)
			if !c.valid || err != nil || c.options {
				return nil, errors.New("native OSPF binding is malformed or has additional options")
			}
			if current == id {
				continue
			}
		}
		// Retain scope as well as text so moving a setting to another interface is detected.
		remaining = append(remaining, scopes[i])
	}
	return remaining, nil
}
