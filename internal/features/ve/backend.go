package ve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"time"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/features/vlan"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type config struct {
	ID, VLANID int64
	PortName   string
}

func validate(v config) error {
	if v.ID < 1 || v.ID > 4095 {
		return errors.New("ve_id must be between 1 and 4095")
	}
	if v.ID != v.VLANID {
		return errors.New("ve_id and vlan_id must match the FastIron routed VLAN identity")
	}
	if err := interfaceid.ValidatePortName(v.PortName); err != nil {
		return err
	}
	return nil
}

func Read(ctx context.Context, d *fastiron.Device, id int64) (config, error) {
	_, err := readCached(ctx, d, id)
	if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		return config{}, err
	}

	native, err := readNative(ctx, d, id)
	if err != nil {
		return config{}, err
	}
	if !native.Exists {
		return config{}, fastiron.ErrNotFound
	}
	return config{ID: id, VLANID: id, PortName: native.PortName}, nil
}

func readNative(ctx context.Context, d *fastiron.Device, id int64) (nativeconfig.VE, error) {
	output, err := d.RunningConfig(ctx)
	if err != nil {
		return nativeconfig.VE{}, err
	}
	document, err := nativeconfig.Parse(output)
	if err != nil {
		return nativeconfig.VE{}, err
	}
	return document.VE(id)
}

func readCached(ctx context.Context, d *fastiron.Device, id int64) (config, error) {
	if err := validate(config{ID: id, VLANID: id}); err != nil {
		return config{}, err
	}
	if !d.RESTCONFEnabled() {
		return config{}, errors.New("VE configuration currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name   string `json:"name"`
				Config *struct {
					Name        string `json:"name"`
					Type        string `json:"type"`
					Description string `json:"description"`
				} `json:"config"`
				Routed *struct {
					Config *struct {
						VLAN int64 `json:"vlan"`
					} `json:"config"`
				} `json:"openconfig-vlan:routed-vlan"`
			} `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.DoREST(ctx, http.MethodGet, "/interfaces", nil, &response); err != nil {
		return config{}, err
	}
	if response.Interfaces == nil {
		return config{}, errors.New("RESTCONF interface collection is missing its container")
	}
	if len(response.Interfaces.Interface) == 0 {
		return config{}, errors.New("RESTCONF interface collection is empty; cannot confirm VE state")
	}
	name := "ve " + strconv.FormatInt(id, 10)
	var observed *config
	for _, entry := range response.Interfaces.Interface {
		if entry.Name != name {
			continue
		}
		if entry.Config == nil || entry.Config.Name != name || entry.Config.Type != "iana-if-type:l3ipvlan" || entry.Routed == nil || entry.Routed.Config == nil {
			return config{}, errors.New("RESTCONF VE response is missing its identity or VLAN binding")
		}
		if observed != nil {
			return config{}, errors.New("RESTCONF interface collection repeats the requested VE")
		}
		if entry.Routed.Config.VLAN != id {
			return config{}, errors.New("RESTCONF VE VLAN binding does not match its identity")
		}
		observed = &config{ID: id, VLANID: entry.Routed.Config.VLAN, PortName: entry.Config.Description}
	}
	if observed == nil {
		return config{}, fastiron.ErrNotFound
	}
	return *observed, nil
}

func check(ctx context.Context, d *fastiron.Device, v config) error {
	if err := validate(v); err != nil {
		return err
	}
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err := Read(ctx, d, v.ID)
	if errors.Is(err, fastiron.ErrNotFound) {
		return nil
	}
	return err
}

func apply(ctx context.Context, d *fastiron.Device, v config) (*config, error) {
	if err := validate(v); err != nil {
		return nil, err
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return nil, err
	}
	if _, err := vlan.Read(ctx, d, v.VLANID); err != nil {
		return nil, fmt.Errorf("VE requires an existing VLAN: %w", err)
	}
	cached, cacheErr := readCached(ctx, d, v.ID)
	if cacheErr != nil && !errors.Is(cacheErr, fastiron.ErrNotFound) {
		return nil, cacheErr
	}
	before, err := readNative(ctx, d, v.ID)
	if err != nil {
		return nil, err
	}
	current := before
	observed := func() *config {
		if !current.Exists {
			return nil
		}
		return &config{ID: v.ID, VLANID: v.ID, PortName: current.PortName}
	}
	ctx, cancel := context.WithTimeout(ctx, d.RESTCONFTimeout())
	defer cancel()

	for {
		if current.Exists && current.PortName == v.PortName {
			return observed(), d.Persist(ctx)
		}
		// Scalar DELETE removes native-only names even when the cached name is empty.
		if current.Exists && v.PortName == "" {
			break
		}
		cacheAbsent := errors.Is(cacheErr, fastiron.ErrNotFound)
		if (!current.Exists && cacheAbsent) || (current.Exists && !cacheAbsent && cached.PortName == current.PortName) {
			break
		}
		// PUT can acknowledge an unchanged cached value without invoking native configuration.
		select {
		case <-ctx.Done():
			return observed(), errors.Join(errors.New("VE configuration cache did not synchronize"), ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
		cached, cacheErr = readCached(ctx, d, v.ID)
		if cacheErr != nil && !errors.Is(cacheErr, fastiron.ErrNotFound) {
			return observed(), cacheErr
		}
		next, err := readNative(ctx, d, v.ID)
		if err != nil {
			return observed(), err
		}
		current = next
		if !slices.Equal(before.Remaining, current.Remaining) {
			return observed(), errors.New("unrelated configuration changed while waiting for VE synchronization")
		}
	}

	name := "ve " + strconv.FormatInt(v.ID, 10)
	target := path.Join("/openconfig-interfaces:interfaces/interface", url.PathEscape(name), "config/description")
	method := http.MethodPut
	var body any = map[string]string{"openconfig-interfaces:description": v.PortName}
	if v.PortName == "" {
		method, body = http.MethodDelete, nil
	}
	if !current.Exists {
		method, target = http.MethodPost, "/interfaces"
		entry := map[string]any{"name": name, "config": map[string]any{"name": name, "type": "iana-if-type:l3ipvlan", "description": v.PortName}, "openconfig-vlan:routed-vlan": map[string]any{"config": map[string]any{"vlan": v.VLANID}}}
		body = map[string]any{"interface": []any{entry}}
	}
	writeErr := d.DoREST(ctx, method, target, body, nil)
	for {
		next, readErr := readNative(ctx, d, v.ID)
		if readErr != nil {
			return observed(), errors.Join(writeErr, readErr)
		}
		current = next
		// Preserve all unowned native commands before allowing any save, including after partial failure.
		if !slices.Equal(before.Remaining, current.Remaining) {
			return observed(), errors.Join(writeErr, errors.New("VE mutation changed unrelated configuration"))
		}
		if writeErr != nil {
			return observed(), writeErr
		}
		if current.Exists && current.PortName == v.PortName {
			return observed(), d.Persist(ctx)
		}
		select {
		case <-ctx.Done():
			return observed(), errors.Join(errors.New("native VE configuration did not converge"), ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func remove(ctx context.Context, d *fastiron.Device, id int64) error {
	if err := validate(config{ID: id, VLANID: id}); err != nil {
		return err
	}
	unlock, err := d.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err = Read(ctx, d, id)
	if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		return err
	}
	if err == nil {
		// Removing a logical interface can erase addresses, protocol bindings, and
		// other independent configuration. Verify children before deleting the parent.
		output, err := d.RunningConfig(ctx)
		if err != nil {
			return err
		}
		name := "ve " + strconv.FormatInt(id, 10)
		if err := veChildren(output, id); err != nil {
			return err
		}
		writeErr := d.DoREST(ctx, http.MethodDelete, path.Join("/interfaces", "interface="+url.PathEscape(name)), nil, nil)
		_, readErr := Read(ctx, d, id)
		if !errors.Is(readErr, fastiron.ErrNotFound) {
			return errors.Join(writeErr, readErr, errors.New("VE absence could not be verified"))
		}
		if writeErr != nil {
			return writeErr
		}
	}
	return d.Persist(ctx)
}

func veChildren(config string, id int64) error {
	document, err := nativeconfig.Parse(config)
	if err != nil {
		return err
	}

	state, err := document.VE(id)
	if err != nil {
		return err
	}
	if !state.Exists {
		return errors.New("cannot confirm the VE configuration block before deletion")
	}
	if state.HasChildren {
		return errors.New("VE has child configuration; remove addresses, routing bindings, and other settings before destroying it")
	}

	return nil
}
