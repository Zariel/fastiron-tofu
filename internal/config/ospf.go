package config

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"slices"
	"strconv"
)

// OSPFAreas returns default-VRF area identities and their native interface bindings.
// Area options and interface options remain independently owned.
func (d *Document) OSPFAreas() (map[netip.Addr][]string, error) {
	areas := map[netip.Addr][]string{}
	declared := map[netip.Addr]bool{}
	vrfs := map[int]bool{}
	process := -1
	for i, c := range d.Commands {
		if c.kind == interfaceVRF && c.Parent >= 0 {
			if !c.valid || vrfs[c.Parent] {
				return nil, errors.New("native interface VRF is malformed or repeated")
			}
			vrfs[c.Parent] = true
		}
		if c.kind == ospfRouter && c.Parent == -1 {
			if !c.valid {
				return nil, errors.New("native OSPF process is malformed")
			}
			if c.name != "" {
				continue
			}
			if process >= 0 {
				return nil, errors.New("native configuration repeats the default OSPF process")
			}
			process = i
		}
		if process < 0 || c.Parent != process || c.kind != ospfArea {
			continue
		}
		id, err := ospfID(c.name)
		if !c.valid || err != nil || declared[id] && !c.options {
			return nil, errors.New("native OSPF area is malformed or repeated")
		}
		declared[id] = declared[id] || !c.options
		areas[id] = []string{}
	}
	bindings := map[string]bool{}
	for _, c := range d.Commands {
		if c.kind != ospfBinding || c.Parent < 0 || vrfs[c.Parent] {
			continue
		}
		parent := d.Commands[c.Parent]
		if parent.kind != interfaceStanza {
			continue
		}
		id, err := ospfID(c.name)
		if !parent.valid || !c.valid || err != nil || bindings[parent.name] {
			return nil, errors.New("native OSPF interface binding is malformed or repeated")
		}
		if _, exists := areas[id]; !exists {
			return nil, errors.New("native OSPF interface references an undeclared area")
		}
		bindings[parent.name] = true
		areas[id] = append(areas[id], parent.name)
	}
	for _, names := range areas {
		slices.Sort(names)
	}
	return areas, nil
}

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
	areas, err := d.OSPFAreas()
	if err != nil {
		return err
	}
	bindings, found := areas[wanted]
	if !found {
		return errors.New("OSPF area was not found in native configuration")
	}
	if len(bindings) > 0 {
		return errors.New("native OSPF configuration still contains an area binding")
	}
	for _, c := range d.Commands {
		if c.Parent < 0 || c.kind != ospfArea || !c.options {
			continue
		}
		parent := d.Commands[c.Parent]
		if parent.kind != ospfRouter || parent.name != "" {
			continue
		}
		current, err := ospfID(c.name)
		if err != nil {
			return err
		}
		if current == wanted {
			return errors.New("OSPF area has additional native options; remove them before destroying the area")
		}
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
		if c.kind == interfaceVRF {
			return errors.New("OSPF interface belongs to another VRF; default-VRF binding deletion is not permitted")
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
