package protectedport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type nativeState struct {
	enabled bool
	unowned []string
}

type interfaceEntry struct {
	Name   string `json:"name"`
	Config *struct {
		Name string `json:"name"`
	} `json:"config"`
	Ethernet *struct {
		Config *struct {
			Aggregate string `json:"openconfig-if-aggregate:aggregate-id"`
		} `json:"config"`
	} `json:"openconfig-if-ethernet:ethernet"`
}

func validateInterface(name string) error {
	if interfaceid.LAG(name) || strings.HasPrefix(name, "ethernet ") && interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
		return nil
	}
	return errors.New("interface must be a canonical Ethernet or LAG name")
}

func read(ctx context.Context, device *fastiron.Device, name string) (nativeState, error) {
	if err := validateInterface(name); err != nil {
		return nativeState{}, err
	}
	if _, err := device.Discover(ctx); err != nil {
		return nativeState{}, err
	}
	// Protection defaults are meaningful only after confirming the parent exists.
	var interfaces struct {
		Collection *struct {
			Entries []interfaceEntry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := device.ReadREST(ctx, "/interfaces", &interfaces); err != nil {
		return nativeState{}, err
	}
	if interfaces.Collection == nil || len(interfaces.Collection.Entries) == 0 {
		return nativeState{}, errors.New("RESTCONF interface collection is missing or empty")
	}
	found := false
	for _, entry := range interfaces.Collection.Entries {
		if entry.Name != name {
			continue
		}
		if found || entry.Config == nil || entry.Config.Name != name {
			return nativeState{}, errors.New("RESTCONF protected-port parent identity is inconsistent")
		}
		if entry.Ethernet != nil && entry.Ethernet.Config != nil && entry.Ethernet.Config.Aggregate != "" {
			return nativeState{}, fmt.Errorf("%s is a LAG member; manage or query protected-port configuration on %s", name, entry.Ethernet.Config.Aggregate)
		}
		found = true
	}
	if !found {
		return nativeState{}, fastiron.ErrNotFound
	}
	var response struct {
		Protected json.RawMessage `json:"icx-openconfig-pp:protectedport"`
	}
	if err := device.ReadREST(ctx, "/protectedport", &response); err != nil {
		return nativeState{}, err
	}
	if len(response.Protected) == 0 || string(response.Protected) == "null" {
		return nativeState{}, errors.New("RESTCONF protected-port response omitted its container")
	}
	// This endpoint caches configured entries and omits native-only protection.
	// Native configuration determines drift; the GET establishes RESTCONF availability.
	document, err := device.RunningConfig(ctx)
	if err != nil {
		return nativeState{}, err
	}
	return parse(document, name)
}

func parse(document *config.Document, name string) (nativeState, error) {
	observed, err := document.InterfacePolicy(name, config.Protection)
	if err != nil {
		return nativeState{}, err
	}
	return nativeState{enabled: observed.Enabled, unowned: observed.Remaining}, nil
}

func apply(ctx context.Context, device *fastiron.Device, name string, desired bool) (*bool, error) {
	if err := validateInterface(name); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, device, func(update *fastiron.Update) (*bool, error) {
		current, err := read(ctx, device, name)
		if errors.Is(err, fastiron.ErrNotFound) && !desired {
			// A deleted parent has no remaining protection, but a pending save must finish.
			return &current.enabled, nil
		}
		if err != nil {
			return nil, err
		}
		if current.enabled == desired {
			return &current.enabled, nil
		}

		before := current
		leaf := path.Join("/protectedport/interfaces", "interface="+url.PathEscape(name))
		write := func(method string) error {
			target := leaf
			var body any
			if method == http.MethodPatch {
				target = "/protectedport"
				entry := map[string]any{"name": name, "config": map[string]any{"name": name, "protectedport": true}}
				body = map[string]any{"protectedport": map[string]any{"interfaces": map[string]any{"interface": entry}}}
			}
			var writeErr error
			if method == http.MethodDelete {
				writeErr = update.DeleteIfPresent(target)
			} else {
				writeErr = update.REST(method, target, body)
			}
			document, readErr := device.RunningConfig(ctx)
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			observed, readErr := parse(document, name)
			if readErr != nil {
				return errors.Join(writeErr, readErr)
			}
			current = observed
			// Verify each narrow mutation before saving; never reconstruct unrelated settings.
			if !slices.Equal(before.unowned, current.unowned) {
				return errors.Join(writeErr, errors.New("protected-port mutation changed unrelated configuration"))
			}
			return writeErr
		}

		// LAG callbacks can skip an unchanged cached value after native drift.
		if err := write(http.MethodDelete); err != nil {
			return &current.enabled, err
		}
		if desired || current.enabled {
			// Native-only protection needs a RESTCONF entry before it can be deleted.
			if err := write(http.MethodPatch); err != nil {
				return &current.enabled, err
			}
		}
		if !desired && current.enabled {
			if err := write(http.MethodDelete); err != nil {
				return &current.enabled, err
			}
		}
		if current.enabled != desired {
			return &current.enabled, errors.New("native protected-port configuration did not converge")
		}
		return &current.enabled, nil
	})
}
