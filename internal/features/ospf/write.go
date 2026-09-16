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
		cached, err := cachedAreas(ctx, d)
		createProtocol := errors.Is(err, fastiron.ErrNotFound)
		if err != nil && !createProtocol {
			return nil, err
		}
		areas, before, err := nativeAreas(ctx, d)
		if err != nil {
			return nil, err
		}
		// REST identities select request paths; native configuration determines ownership and convergence.
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
				if err := before.CheckOSPFAreaDelete(id); err != nil {
					return current, err
				}
				endpoint = path.Join(ospfAreasPath, "area="+url.PathEscape(areaKey(cached, id)))
				method = http.MethodDelete
			}
			writeErr := update.REST(method, endpoint, body)
			observed, after, readErr := nativeAreas(ctx, d)
			if readErr != nil {
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
			if err := after.CheckOSPFAreaUpdate(before, id); err != nil {
				return current, err
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
		cached, err := cachedAreas(ctx, d)
		if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
			return false, err
		}
		areas, before, err := nativeAreas(ctx, d)
		if err != nil {
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
			endpoint := path.Join(ospfAreasPath, "area="+url.PathEscape(areaKey(cached, id)), "interfaces")
			method := http.MethodPost
			var body any = map[string]any{"interface": []any{map[string]any{"id": name, "config": map[string]any{"id": name}}}}
			if !present {
				// Unbinding must not erase independently configured OSPF interface options.
				if err := before.CheckOSPFBindingDelete(id, name); err != nil {
					return exists, err
				}
				endpoint = path.Join(endpoint, "interface="+url.PathEscape(name))
				method = http.MethodDelete
				body = nil
			}
			writeErr := update.REST(method, endpoint, body)
			observed, after, readErr := nativeAreas(ctx, d)
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
			if err := after.CheckOSPFBindingUpdate(before, id, name); err != nil {
				return exists, err
			}
		}
		return exists, nil
	})
}

func areaKey(cached []area, id string) string {
	for _, entry := range cached {
		if entry.ID == id {
			return entry.key
		}
	}
	return id
}
