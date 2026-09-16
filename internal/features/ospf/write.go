package ospf

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func applyArea(ctx context.Context, d *fastiron.Device, id string, present bool) (*area, error) {
	if err := validateAreaID(id); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*area, error) {
		areas, err := configuredAreas(ctx, d)
		createProtocol := errors.Is(err, fastiron.ErrNotFound)
		if err != nil && !createProtocol {
			return nil, err
		}
		var current *area
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
					return current, errors.New("OSPF area still has interface bindings; remove them before destroying the area")
				}
				document, err := d.RunningConfig(ctx)
				if err != nil {
					return current, err
				}
				if err := document.CheckOSPFAreaDelete(id); err != nil {
					return current, err
				}
				endpoint = path.Join(ospfAreasPath, "area="+url.PathEscape(current.key))
				method = http.MethodDelete
			}
			writeErr := update.REST(method, endpoint, body)
			observed, readErr := configuredAreas(ctx, d)
			if readErr != nil && !errors.Is(readErr, fastiron.ErrNotFound) {
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
				index := slices.IndexFunc(observed, func(area area) bool { return area.ID == neighbor.ID })
				if index < 0 || !slices.Equal(observed[index].Interfaces, neighbor.Interfaces) {
					return nil, errors.New("OSPF area operation changed an unrelated area")
				}
			}
		}
		return current, nil
	})
}

func applyInterface(ctx context.Context, d *fastiron.Device, id, name string, present bool) (bool, error) {
	if err := validateAreaID(id); err != nil {
		return false, err
	}
	if err := validateInterface(name); err != nil {
		return false, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (bool, error) {
		areas, err := configuredAreas(ctx, d)
		if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
			return false, err
		}
		var target *area
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
				document, err := d.RunningConfig(ctx)
				if err != nil {
					return exists, err
				}
				if err := document.CheckOSPFBindingDelete(id, name); err != nil {
					return exists, err
				}
				endpoint = path.Join(endpoint, "interface="+url.PathEscape(name))
				method = http.MethodDelete
				body = nil
			}
			writeErr := update.REST(method, endpoint, body)
			observed, readErr := configuredAreas(ctx, d)
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
				index := slices.IndexFunc(observed, func(area area) bool { return area.ID == neighbor.ID })
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
		return exists, nil
	})
}
