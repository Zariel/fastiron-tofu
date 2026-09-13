package vlan

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type defaultState struct {
	id                          int64
	vlans                       map[int64]bool
	properties, routed, unowned []string
}

func readDefault(ctx context.Context, device *fastiron.Device) (defaultState, error) {
	if _, err := device.Discover(ctx); err != nil {
		return defaultState{}, err
	}
	configuration, err := device.RunningConfig(ctx)
	if err != nil {
		return defaultState{}, err
	}
	return nativeDefault(configuration)
}

func nativeDefault(configuration string) (defaultState, error) {
	document, err := config.Parse(configuration)
	if err != nil {
		return defaultState{}, err
	}
	id, err := document.DefaultVLAN()
	if err != nil {
		return defaultState{}, err
	}
	state := defaultState{id: id, vlans: map[int64]bool{}}
	active := false
	routed := false
	for _, command := range document.Commands {
		line := command.Text
		if command.Parent == -1 {
			active = false
			routed = false
		}
		if command.Parent == -1 && strings.HasPrefix(line, "default-vlan-id ") {
			continue
		}
		// The native default VLAN move also renumbers its associated VE.
		// Preserve that interface's configuration independently of its position.
		if command.Parent == -1 && line == "interface ve "+strconv.FormatInt(id, 10) {
			routed = true
			state.routed = append(state.routed, "interface ve")
			continue
		}
		if routed {
			state.routed = append(state.routed, line)
			continue
		}
		if command.Parent == -1 && strings.HasPrefix(line, "vlan ") {
			fields := command.Fields
			vlanID, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || vlanID < 1 || vlanID > 4095 || state.vlans[vlanID] {
				return defaultState{}, errors.New("invalid or duplicate native VLAN identity")
			}
			state.vlans[vlanID] = true
			active = vlanID == id
			if active {
				state.properties = append(state.properties, strings.Join(fields[2:], " "))
				continue
			}
		}
		if active {
			state.properties = append(state.properties, line)
			continue
		}
		state.unowned = append(state.unowned, line)
	}
	if !state.vlans[id] {
		return defaultState{}, errors.New("native configuration omitted the default VLAN")
	}
	return state, nil
}

func (state defaultState) preserves(previous defaultState) bool {
	return slices.Equal(state.properties, previous.properties) && slices.Equal(state.routed, previous.routed) && slices.Equal(state.unowned, previous.unowned)
}

func cleanDefaultEntries(ctx context.Context, device *fastiron.Device, update *fastiron.Update, current defaultState) error {
	inventory, err := readAll(ctx, device)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(inventory)) {
		entry := inventory[key]
		if entry.Name != "DEFAULT-VLAN" || current.vlans[entry.ID] {
			continue
		}
		// Only orphaned default entries belong to this cleanup. A native VLAN,
		// even one bearing the reserved name, must never be deleted here.
		observed, err := readDefault(ctx, device)
		if err != nil {
			return err
		}
		if observed.id != current.id || !observed.preserves(current) {
			return errors.New("native configuration changed during default VLAN reconciliation; retry")
		}
		writeErr := update.DeleteIfPresent(path.Join(vlanPath, "vlan="+key))
		observed, err = readDefault(ctx, device)
		if err != nil {
			return errors.Join(writeErr, err)
		}
		if observed.id != current.id || !observed.preserves(current) {
			return errors.Join(writeErr, errors.New("default VLAN metadata cleanup changed native configuration"))
		}
		if writeErr != nil {
			return writeErr
		}
		remaining, err := readAll(ctx, device)
		if err != nil {
			return err
		}
		if _, exists := remaining[key]; exists {
			return fmt.Errorf("RESTCONF retained former default VLAN %s; synchronize and retry", key)
		}
	}
	return nil
}

func applyDefault(ctx context.Context, device *fastiron.Device, desired int64) (*int64, error) {
	if desired < 1 || desired > 4095 {
		return nil, errors.New("default vlan_id must be between 1 and 4095")
	}
	if !device.RESTCONFEnabled() {
		return nil, errors.New("default VLAN selection requires RESTCONF")
	}
	return fastiron.Reconcile(ctx, device, func(update *fastiron.Update) (*int64, error) {
		current, err := readDefault(ctx, device)
		if err != nil {
			return nil, err
		}
		if desired != current.id && current.vlans[desired] {
			return nil, errors.New("default VLAN target is already in use; select an unused VLAN ID")
		}
		if err := cleanDefaultEntries(ctx, device, update, current); err != nil {
			return &current.id, err
		}
		if desired == current.id {
			return &current.id, nil
		}
		inventory, err := readAll(ctx, device)
		if err != nil {
			return &current.id, err
		}
		if _, exists := inventory[strconv.FormatInt(desired, 10)]; exists {
			return &current.id, errors.New("default VLAN target still exists in RESTCONF; synchronize and retry")
		}
		observed, err := readDefault(ctx, device)
		if err != nil {
			return &current.id, err
		}
		if observed.id != current.id || !observed.preserves(current) {
			return &observed.id, errors.New("native configuration changed before default VLAN selection; retry")
		}
		entry := vlanEntry{ID: desired}
		entry.Config.ID, entry.Config.Name = desired, "DEFAULT-VLAN"
		body := map[string]any{"vlans": map[string]any{"vlan": []vlanEntry{entry}}}
		writeErr := update.REST(http.MethodPatch, vlanPath, body)
		observed, err = readDefault(ctx, device)
		if err != nil {
			return &current.id, errors.Join(writeErr, err)
		}
		if observed.id != desired || !observed.preserves(current) {
			return &observed.id, errors.Join(writeErr, errors.New("default VLAN selection did not preserve native configuration"))
		}
		if writeErr != nil {
			return &observed.id, writeErr
		}
		if err := cleanDefaultEntries(ctx, device, update, observed); err != nil {
			return &observed.id, err
		}
		return &observed.id, nil
	})
}
