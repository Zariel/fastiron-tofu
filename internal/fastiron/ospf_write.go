package fastiron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
)

func (d *Device) ApplyOSPFArea(ctx context.Context, id string, present bool) (*OSPFArea, error) {
	if err := ValidateOSPFAreaID(id); err != nil {
		return nil, err
	}
	unlock, err := d.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	areas, err := d.readOSPFAreas(ctx)
	createProtocol := errors.Is(err, ErrNotFound)
	if err != nil && !createProtocol {
		return nil, err
	}
	var current *ospfArea
	for _, area := range areas {
		if area.ID == id {
			current = &area
		}
	}
	if (current != nil) != present {
		endpoint := protocolsPath
		method := http.MethodPatch
		var body any
		if present {
			protocol := map[string]any{"identifier": "OSPF", "name": "icx-ospf", "config": map[string]any{"identifier": "OSPF", "name": "icx-ospf"}, "ospfv2": map[string]any{"areas": map[string]any{"area": []any{map[string]any{"identifier": id, "config": map[string]any{"identifier": id}}}}}}
			body = map[string]any{"protocols": map[string]any{"protocol": []any{protocol}}}
			if createProtocol {
				method = http.MethodPost
				body = map[string]any{"protocol": []any{protocol}}
			}
		} else {
			if len(current.Interfaces) > 0 {
				return &current.OSPFArea, errors.New("OSPF area still has interface bindings; remove them before destroying the area")
			}
			output, err := d.cli.Run(ctx, true, "show running-config")
			if err != nil {
				return &current.OSPFArea, err
			}
			if err := ospfAreaChildren(output[0], id); err != nil {
				return &current.OSPFArea, err
			}
			endpoint = path.Join(ospfAreasPath, "area="+url.PathEscape(current.key))
			method = http.MethodDelete
		}
		writeErr := d.rest.Do(ctx, method, endpoint, body, nil)
		observed, readErr := d.readOSPFAreas(ctx)
		if readErr != nil && !errors.Is(readErr, ErrNotFound) {
			return nil, errors.Join(writeErr, readErr)
		}
		current = nil
		for _, area := range observed {
			if area.ID == id {
				current = &area
			}
		}
		if (current != nil) != present {
			return nil, errors.Join(writeErr, errors.New("OSPF area did not converge"))
		}
		// Area operations must preserve independent areas and their bindings.
		for _, neighbor := range areas {
			if neighbor.ID == id {
				continue
			}
			index := slices.IndexFunc(observed, func(area ospfArea) bool { return area.ID == neighbor.ID })
			if index < 0 || !slices.Equal(observed[index].Interfaces, neighbor.Interfaces) {
				return nil, errors.New("OSPF area operation changed an unrelated area")
			}
		}
	}
	var result *OSPFArea
	if current != nil {
		result = &current.OSPFArea
	}
	if d.config.Persistence == "after_each_write" {
		return result, d.save(ctx)
	}
	return result, nil
}

func ospfAreaChildren(config, id string) error {
	if _, err := configuration(config); err != nil {
		return err
	}
	inside, found := false, false
	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 4 && fields[0] == "ip" && fields[1] == "ospf" && fields[2] == "area" {
			bound, err := normalizeAreaID(fields[3])
			if err != nil || bound == id {
				return errors.New("native OSPF configuration still contains an area binding")
			}
		}
		if line == "router ospf" {
			inside = true
			continue
		}
		if line != "" && line != "!" && !strings.HasPrefix(line, " ") {
			inside = false
		}
		if !inside {
			continue
		}
		if len(fields) < 2 || fields[0] != "area" {
			continue
		}
		areaID, err := normalizeAreaID(fields[1])
		if err != nil {
			return fmt.Errorf("cannot identify native OSPF area: %w", err)
		}
		if areaID != id {
			continue
		}
		found = true
		if len(fields) > 2 {
			return errors.New("OSPF area has additional native options; remove them before destroying the area")
		}
	}
	if !found {
		return errors.New("OSPF area was not found in native configuration")
	}
	return nil
}

func (d *Device) ApplyOSPFInterface(ctx context.Context, id, name string, present bool) (bool, error) {
	if err := ValidateOSPFAreaID(id); err != nil {
		return false, err
	}
	if err := ValidateOSPFInterface(name); err != nil {
		return false, err
	}
	unlock, err := d.lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return false, err
	}
	areas, err := d.readOSPFAreas(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	var target *ospfArea
	exists := false
	for _, area := range areas {
		if area.ID == id {
			target = &area
			exists = slices.Contains(area.Interfaces, name)
		} else if present && slices.Contains(area.Interfaces, name) {
			return false, errors.New("interface is already bound to another OSPF area")
		}
	}
	if present && target == nil {
		return false, errors.New("OSPF area does not exist; create it before binding the interface")
	}
	if exists != present {
		endpoint := path.Join(ospfAreasPath, "area="+url.PathEscape(target.key), "interfaces")
		method := http.MethodPost
		var body any = map[string]any{"interface": []any{map[string]any{"id": name, "config": map[string]any{"id": name}}}}
		if !present {
			// Unbinding must not erase independently configured OSPF interface options.
			output, err := d.cli.Run(ctx, true, "show running-config")
			if err != nil {
				return exists, err
			}
			if err := ospfInterfaceOptions(output[0], id, name); err != nil {
				return exists, err
			}
			endpoint = path.Join(endpoint, "interface="+url.PathEscape(name))
			method = http.MethodDelete
			body = nil
		}
		writeErr := d.rest.Do(ctx, method, endpoint, body, nil)
		observed, readErr := d.readOSPFAreas(ctx)
		if readErr != nil {
			return exists, errors.Join(writeErr, readErr)
		}
		exists = false
		for _, area := range observed {
			if area.ID == id {
				exists = slices.Contains(area.Interfaces, name)
			}
		}
		if exists != present {
			return exists, errors.Join(writeErr, errors.New("OSPF interface binding did not converge"))
		}
		for _, neighbor := range areas {
			index := slices.IndexFunc(observed, func(area ospfArea) bool { return area.ID == neighbor.ID })
			if index < 0 {
				return exists, errors.New("OSPF binding operation removed an area")
			}
			for _, other := range neighbor.Interfaces {
				if neighbor.ID == id && other == name {
					continue
				}
				if !slices.Contains(observed[index].Interfaces, other) {
					return exists, errors.New("OSPF binding operation changed an unrelated interface")
				}
			}
		}
	}
	if d.config.Persistence == "after_each_write" {
		return exists, d.save(ctx)
	}
	return exists, nil
}

func ospfInterfaceOptions(config, id, name string) error {
	if _, err := configuration(config); err != nil {
		return err
	}
	inside, found := false, false
	for _, line := range strings.Split(config, "\n") {
		if line == "interface "+name {
			inside = true
			continue
		}
		if line != "" && line != "!" && !strings.HasPrefix(line, " ") {
			inside = false
		}
		if !inside {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "ip" || fields[1] != "ospf" {
			continue
		}
		if len(fields) != 4 || fields[2] != "area" {
			return errors.New("interface has additional OSPF options; remove them before destroying its area binding")
		}
		areaID, err := normalizeAreaID(fields[3])
		if err != nil || areaID != id {
			return errors.New("native OSPF interface binding differs from RESTCONF")
		}
		found = true
	}
	if !found {
		return errors.New("OSPF interface binding was not found in native configuration")
	}
	return nil
}
