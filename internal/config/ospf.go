package config

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"strconv"
)

func ospfID(raw string) (netip.Addr, error) {
	if id, err := netip.ParseAddr(raw); err == nil && id.Is4() {
		return id, nil
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return netip.Addr{}, errors.New("invalid native OSPF area identifier")
	}
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], uint32(n))
	return netip.AddrFrom4(bytes), nil
}

// CheckOSPFAreaDelete rejects deletion of a default-VRF area with bindings or
// independently owned native options.
func (d *Document) CheckOSPFAreaDelete(id string) error {
	wanted, err := ospfID(id)
	if err != nil {
		return err
	}
	found := false
	for _, c := range d.Commands {
		if c.Parent < 0 {
			continue
		}
		parent := d.Commands[c.Parent]
		binding := c.kind == ospfBinding && parent.kind == interfaceStanza
		area := c.kind == ospfArea && parent.kind == ospfRouter && parent.valid && parent.name == ""
		if !binding && !area {
			continue
		}
		if !c.valid {
			return errors.New("native OSPF area or binding is malformed")
		}
		current, err := ospfID(c.name)
		if err != nil {
			return err
		}
		if current != wanted {
			continue
		}
		if binding {
			return errors.New("native OSPF configuration still contains an area binding")
		}
		if c.options {
			return errors.New("OSPF area has additional native options; remove them before destroying the area")
		}
		if found {
			return errors.New("native OSPF configuration repeats the area")
		}
		found = true
	}
	if !found {
		return errors.New("OSPF area was not found in native configuration")
	}
	return nil
}

// CheckOSPFBindingDelete protects interface OSPF options from binding deletion.
func (d *Document) CheckOSPFBindingDelete(id, name string) error {
	wanted, err := ospfID(id)
	if err != nil {
		return err
	}
	header, err := d.interfaceHeader(name)
	if err != nil {
		return err
	}
	found := false
	for _, c := range d.Commands {
		if header < 0 || c.Parent != header {
			continue
		}
		if c.kind == ospfOption || c.kind == ospfBinding && c.options {
			return errors.New("interface has additional OSPF options; remove them before destroying its area binding")
		}
		if c.kind != ospfBinding {
			continue
		}
		current, err := ospfID(c.name)
		if !c.valid || err != nil || current != wanted || found {
			return errors.New("native OSPF interface binding differs from RESTCONF or is malformed or repeated")
		}
		found = true
	}
	if !found {
		return errors.New("OSPF interface binding was not found in native configuration")
	}
	return nil
}
