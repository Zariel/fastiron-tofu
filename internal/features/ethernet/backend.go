package ethernet

import (
	"context"
	"errors"
	"net/http"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type config struct {
	Port, PortName string
	Enabled        bool
}

func validate(v config) error {
	if !interfaceid.EthernetPort(v.Port) {
		return errors.New("port must use stack/slot/port syntax with positive numbers and no leading zeros")
	}
	return interfaceid.ValidatePortName(v.PortName)
}

type portFields struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
}

type portEntry struct {
	Name   string      `json:"name"`
	Config *portFields `json:"config"`
	State  *portFields `json:"state"`
}

// CheckPort verifies physical-port identity without requiring configuration observations.
func CheckPort(ctx context.Context, d *fastiron.Device, port string) error {
	_, err := readEntry(ctx, d, port)
	return err
}

func readEntry(ctx context.Context, d *fastiron.Device, port string) (portEntry, error) {
	if err := validate(config{Port: port}); err != nil {
		return portEntry{}, err
	}
	if !d.RESTCONFEnabled() {
		return portEntry{}, errors.New("base Ethernet configuration currently requires RESTCONF")
	}
	var response struct {
		Interfaces *struct {
			Interface []portEntry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.ReadREST(ctx, "/interfaces", &response); err != nil {
		return portEntry{}, err
	}
	if response.Interfaces == nil || len(response.Interfaces.Interface) == 0 {
		return portEntry{}, errors.New("RESTCONF interface collection is missing or empty; cannot confirm Ethernet state")
	}
	var found *portEntry
	for _, entry := range response.Interfaces.Interface {
		if entry.Name != "ethernet "+port {
			continue
		}
		if found != nil {
			return portEntry{}, errors.New("RESTCONF interface collection repeats the requested Ethernet identity")
		}
		if entry.Config == nil || entry.Config.Name != entry.Name {
			return portEntry{}, errors.New("RESTCONF Ethernet response has an inconsistent configured identity")
		}
		found = &entry
	}
	if found == nil {
		return portEntry{}, fastiron.ErrNotFound
	}
	return *found, nil
}

func (e portEntry) current(port string) (config, error) {
	// State is native-derived; config can retain deleted values after CLI restoration.
	if e.State == nil || e.State.Description == nil || e.State.Enabled == nil {
		return config{}, errors.New("RESTCONF Ethernet response omitted native description or enable state")
	}
	if e.State.Name != "" && e.State.Name != e.Name {
		return config{}, errors.New("RESTCONF Ethernet native identity disagrees with configuration")
	}
	return config{Port: port, PortName: *e.State.Description, Enabled: *e.State.Enabled}, nil
}

func read(ctx context.Context, d *fastiron.Device, port string) (config, error) {
	entry, err := readEntry(ctx, d, port)
	if err != nil {
		return config{}, err
	}
	return entry.current(port)
}

func check(ctx context.Context, d *fastiron.Device, v config) error {
	if err := validate(v); err != nil {
		return err
	}
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	_, err := read(ctx, d, v.Port)
	return err
}

func apply(ctx context.Context, d *fastiron.Device, v config) (*config, error) {
	if err := validate(v); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*config, error) {
		entry, err := readEntry(ctx, d, v.Port)
		if err != nil {
			return nil, err
		}
		current, err := entry.current(v.Port)
		if err != nil {
			return nil, err
		}
		values, prime := map[string]any{}, map[string]any{}
		if current.PortName != v.PortName {
			values["description"] = v.PortName
			if entry.Config.Description != nil && *entry.Config.Description == v.PortName {
				prime["description"] = current.PortName
			}
		}
		if current.Enabled != v.Enabled {
			values["enabled"] = v.Enabled
			if entry.Config.Enabled != nil && *entry.Config.Enabled == v.Enabled {
				prime["enabled"] = current.Enabled
			}
		}
		if len(prime) != 0 {
			// Align cached fields with existing native values before requesting a cached
			// desired value. This forces the callback without changing native settings first.
			before := current
			observed, writeErr := patch(ctx, d, update, v.Port, prime)
			if observed == nil {
				return nil, writeErr
			}
			current = *observed
			if writeErr != nil {
				return &current, writeErr
			}
			if current != before {
				return &current, errors.New("Ethernet cache synchronization changed native configuration")
			}
		}
		if len(values) != 0 {
			observed, writeErr := patch(ctx, d, update, v.Port, values)
			if observed == nil {
				return nil, writeErr
			}
			current = *observed
			if current != v {
				return &current, errors.Join(writeErr, errors.New("Ethernet configuration did not converge"))
			}
		}
		return &current, nil
	})
}

func patch(ctx context.Context, d *fastiron.Device, update *fastiron.Update, port string, values map[string]any) (*config, error) {
	values["name"], values["type"] = "ethernet "+port, "iana-if-type:ethernetCsmacd"
	body := map[string]any{"interfaces": map[string]any{"interface": []any{map[string]any{"name": "ethernet " + port, "config": values}}}}
	writeErr := update.REST(http.MethodPatch, "/interfaces", body)
	observed, readErr := read(ctx, d, port)
	if readErr != nil {
		return nil, errors.Join(writeErr, readErr)
	}
	return &observed, writeErr
}
