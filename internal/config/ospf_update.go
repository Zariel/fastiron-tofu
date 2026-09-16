package config

import (
	"errors"
	"net/netip"
	"slices"
)

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
