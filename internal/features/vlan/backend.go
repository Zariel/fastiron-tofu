package vlan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

type Config struct {
	ID   int64
	Name string
}

const vlanPath = "/network-instances/network-instance=default-vrf/vlans"

type vlanEntry struct {
	ID     int64 `json:"vlan-id"`
	Config struct {
		ID   int64  `json:"vlan-id"`
		Name string `json:"name"`
	} `json:"config"`
}

func Validate(v Config) error {
	if v.ID < 1 || v.ID > 4094 {
		return errors.New("vlan_id must be between 1 and 4094; the active default VLAN is managed separately")
	}
	if len(v.Name) > 32 || !regexp.MustCompile(`^[A-Za-z0-9_.:-]*$`).MatchString(v.Name) {
		return errors.New("VLAN name must contain at most 32 letters, digits, underscores, dots, colons, or hyphens")
	}
	if v.Name == "DEFAULT-VLAN" {
		return errors.New("DEFAULT-VLAN selects the default VLAN; ordinary VLAN resources cannot set it")
	}
	return nil
}

func Read(ctx context.Context, d *fastiron.Device, id int64) (Config, error) {
	if id < 1 || id > 4095 {
		return Config{}, errors.New("vlan_id must be between 1 and 4095")
	}

	if !d.RESTCONFEnabled() {
		return Config{}, errors.New("RESTCONF is required for VLAN reads")
	}
	var response struct {
		VLANs []vlanEntry `json:"openconfig-network-instance:vlan"`
	}
	err := d.ReadREST(ctx, path.Join(vlanPath, "vlan="+strconv.FormatInt(id, 10)), &response)
	if errors.Is(err, restconf.ErrNotFound) {
		// A missing item URL is also how unsupported endpoints can respond. Only
		// a readable parent collection establishes that this identity is absent.
		var collection struct {
			VLANs *struct {
				VLAN []vlanEntry `json:"vlan"`
			} `json:"openconfig-network-instance:vlans"`
		}
		if err := d.ReadREST(ctx, vlanPath, &collection); err != nil {
			return Config{}, err
		}
		if collection.VLANs == nil {
			return Config{}, errors.New("RESTCONF VLAN collection is missing its configuration container")
		}
		for _, entry := range collection.VLANs.VLAN {
			if entry.ID == id {
				if entry.Config.ID != id {
					return Config{}, errors.New("RESTCONF VLAN collection contains an inconsistent identity")
				}
				return Config{ID: id, Name: entry.Config.Name}, nil
			}
		}
		return Config{}, fastiron.ErrNotFound
	}
	if err != nil {
		return Config{}, err
	}
	if len(response.VLANs) != 1 || response.VLANs[0].ID != id || response.VLANs[0].Config.ID != id {
		return Config{}, errors.New("RESTCONF VLAN response is missing the requested identity")
	}
	return Config{ID: id, Name: response.VLANs[0].Config.Name}, nil
}

func check(ctx context.Context, d *fastiron.Device, v Config) error {
	if err := Validate(v); err != nil {
		return err
	}
	if _, err := d.Discover(ctx); err != nil {
		return err
	}
	// A dependency may move the default before this resource applies. Check
	// mutable ownership under the write lock, after dependencies have run.
	_, err := Read(ctx, d, v.ID)
	if errors.Is(err, fastiron.ErrNotFound) {
		return nil
	}
	return err
}

// apply returns observed state even when persistence fails, allowing the
// resource to retain the remote identity for import-free recovery.
func apply(ctx context.Context, d *fastiron.Device, v Config) (*Config, error) {
	if err := Validate(v); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*Config, error) {
		// The global default may have changed since planning. Its implicit VLAN
		// and membership belong to a different configuration domain.
		if err := rejectDefault(ctx, d, v.ID); err != nil {
			return nil, err
		}
		current, err := Read(ctx, d, v.ID)
		if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
			return nil, err
		}
		absent := errors.Is(err, fastiron.ErrNotFound)
		if absent || current != v {
			entry := vlanEntry{ID: v.ID}
			entry.Config.ID = v.ID
			entry.Config.Name = v.Name
			method := http.MethodPatch
			var body any = map[string]any{"vlans": map[string]any{"vlan": []vlanEntry{entry}}}
			if absent {
				method = http.MethodPost
				body = map[string]any{"vlan": []vlanEntry{entry}}
			}
			writeErr := update.REST(method, vlanPath, body)
			// Read after every attempted write, including ambiguous transport failures.
			observed, readErr := Read(ctx, d, v.ID)
			if readErr != nil {
				return nil, errors.Join(writeErr, fmt.Errorf("cannot verify VLAN after write: %w", readErr))
			}
			if observed != v {
				return &observed, errors.Join(writeErr, errors.New("VLAN did not converge to the planned name"))
			}
			current = observed
		}
		// A retry must save even if running state already matches after a failed save.
		return &current, nil
	})
}

func remove(ctx context.Context, d *fastiron.Device, id int64) error {
	if err := Validate(Config{ID: id}); err != nil {
		return err
	}
	return d.Update(ctx, func(update *fastiron.Update) error {
		_, err := Read(ctx, d, id)
		if err != nil && !errors.Is(err, fastiron.ErrNotFound) {
			return err
		}
		if err == nil {
			// Deleting a parent VLAN must not silently erase separately owned children.
			document, err := d.RunningConfig(ctx)
			if err != nil {
				return err
			}
			if err := vlanChildren(document, id); err != nil {
				return err
			}
			writeErr := update.REST(http.MethodDelete, path.Join(vlanPath, "vlan="+strconv.FormatInt(id, 10)), nil)
			_, readErr := Read(ctx, d, id)
			if !errors.Is(readErr, fastiron.ErrNotFound) {
				return errors.Join(writeErr, readErr, errors.New("VLAN absence could not be verified"))
			}
		}
		return nil
	})
}

func vlanChildren(document *nativeconfig.Document, id int64) error {
	defaultVLAN, err := document.DefaultVLAN()
	if err != nil {
		return errors.New("cannot verify VLAN children in running configuration")
	}
	if id == defaultVLAN {
		return errors.New("the default VLAN is not managed by fastiron_vlan")
	}
	inside := false
	for _, command := range document.Commands {
		line := command.Text
		trimmed := strings.TrimSpace(line)
		if trimmed == "interface ve "+strconv.FormatInt(id, 10) {
			return errors.New("VLAN has a routed VE interface; remove it before destroying the VLAN")
		}
		if strings.HasPrefix(line, "vlan ") {
			fields := command.Fields
			inside = len(fields) > 1 && fields[1] == strconv.FormatInt(id, 10)
			continue
		}
		if inside {
			if trimmed == "" || trimmed == "!" {
				continue
			}
			if command.Parent == -1 {
				inside = false
				continue
			}
			return errors.New("VLAN has child configuration; remove memberships, routed interfaces, and spanning-tree settings before destroying it")
		}
	}
	return nil
}
