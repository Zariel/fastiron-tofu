package lag

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/features/ethernet"
	"github.com/zariel/fastiron-tofu/internal/features/vlan"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func validate(v config) error {
	if v.ID < 1 {
		return errors.New("lag_id must be positive")
	}
	if v.Name == "" || len(v.Name) > 64 {
		return errors.New("LAG name must contain 1 to 64 ASCII characters")
	}
	for _, r := range v.Name {
		if r < 32 || r > 126 {
			return errors.New("LAG name must contain printable ASCII characters")
		}
	}
	if v.Mode != "dynamic" && v.Mode != "static" {
		return errors.New("mode must be dynamic or static")
	}

	seen := map[string]bool{}

	for _, name := range v.Members {
		if !strings.HasPrefix(name, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(name, "ethernet ")) {
			return errors.New("LAG members must be canonical Ethernet interface names")
		}
		if seen[name] {
			return errors.New("LAG members must not contain duplicates")
		}
		seen[name] = true
	}
	return nil
}

func readLAG(ctx context.Context, d *fastiron.Device, id int64) (config, error) {
	if id < 1 {
		return config{}, errors.New("lag_id must be positive")
	}

	lags, err := readLAGs(ctx, d)
	if err != nil {
		return config{}, err
	}
	for _, lag := range lags {
		if lag.ID == id {
			return lag, nil
		}
	}
	return config{}, fastiron.ErrNotFound
}

func waitLAG(ctx context.Context, d *fastiron.Device, id int64, matches func(*config) bool) (*config, error) {
	// Bound retries without shortening the independent RESTCONF and SSH operations.
	retryCtx, cancel := context.WithTimeout(ctx, d.RESTCONFTimeout())
	defer cancel()
	for {
		lag, err := readLAG(ctx, d, id)
		var current *config
		if err == nil {
			current = &lag
		} else if !errors.Is(err, fastiron.ErrNotFound) {
			return nil, err
		}
		if matches(current) {
			return current, nil
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return current, fmt.Errorf("LAG configuration did not converge: %w", retryCtx.Err())
		case <-timer.C:
		}
	}
}

// applyLAG reports observed configuration, including after a failed save.
func applyLAG(ctx context.Context, d *fastiron.Device, v config) (*config, error) {
	if err := validate(v); err != nil {
		return nil, err
	}

	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*config, error) {
		lags, err := readLAGs(ctx, d)
		if err != nil {
			return nil, err
		}

		var current *config
		for _, lag := range lags {
			if lag.ID == v.ID {
				current = &lag
				continue
			}
			if lag.Name == v.Name {
				return nil, errors.New("another LAG already uses this name")
			}
			for _, name := range v.Members {
				if slices.Contains(lag.Members, name) {
					return nil, fmt.Errorf("%s already belongs to lag %d; remove that membership first", name, lag.ID)
				}
			}
		}
		if current == nil {
			cached, err := readCollection(ctx, d)
			if err != nil {
				return nil, err
			}
			for _, lag := range cached.lags {
				if lag.ID == v.ID {
					return nil, fmt.Errorf("RESTCONF retains deleted lag %d; restore the parent through CLI before retrying", v.ID)
				}
			}
		}
		if current != nil && current.Mode != v.Mode {
			return nil, errors.New("changing LAG mode requires replacement")
		}

		for _, name := range v.Members {
			if current != nil && slices.Contains(current.Members, name) {
				continue
			}
			if err := ethernet.CheckPort(ctx, d, strings.TrimPrefix(name, "ethernet ")); err != nil {
				return nil, err
			}
			port, err := vlan.ReadSwitchport(ctx, d, name)
			if err != nil {
				return nil, err
			}
			if port.Access > 1 || len(port.Trunks) != 0 {
				return nil, fmt.Errorf("%s has independent VLAN membership; remove it before joining a LAG", name)
			}
		}

		if current == nil || current.Name != v.Name {
			name := "lag " + strconv.FormatInt(v.ID, 10)
			mode := "LACP"
			if v.Mode == "static" {
				mode = "STATIC"
			}
			aggregation := map[string]any{"openconfig-if-aggregate-aug:lag-name": v.Name}
			entry := map[string]any{"name": name, "config": map[string]any{"name": name, "type": "iana-if-type:ieee8023adLag"}, "openconfig-if-aggregate:aggregation": map[string]any{"config": aggregation}}
			method := http.MethodPatch
			body := map[string]any{"interfaces": map[string]any{"interface": []any{entry}}}
			if current == nil {
				aggregation["lag-type"] = mode
				method = http.MethodPost
				body = map[string]any{"interface": []any{entry}}
			}
			writeErr := update.REST(method, "/interfaces", body)
			observed, readErr := waitLAG(ctx, d, v.ID, func(lag *config) bool { return lag != nil && lag.Name == v.Name && lag.Mode == v.Mode })
			if readErr != nil {
				return observed, errors.Join(writeErr, readErr)
			}
			current = observed
		}

		for _, name := range slices.Clone(current.Members) {
			if slices.Contains(v.Members, name) {
				continue
			}
			if err := detachPort(ctx, d, update, v.ID, name); err != nil {
				observed, readErr := readLAG(ctx, d, v.ID)
				if readErr != nil {
					return nil, errors.Join(err, readErr)
				}
				return &observed, err
			}
		}

		for _, name := range v.Members {
			lag, err := readLAG(ctx, d, v.ID)
			if err != nil {
				return current, err
			}
			if slices.Contains(lag.Members, name) {
				continue
			}
			endpoint := path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/config")
			body := map[string]any{"config": map[string]any{"openconfig-if-aggregate:aggregate-id": "lag " + strconv.FormatInt(v.ID, 10)}}
			writeErr := update.REST(http.MethodPatch, endpoint, body)
			observed, readErr := waitLAG(ctx, d, v.ID, func(lag *config) bool { return lag != nil && slices.Contains(lag.Members, name) })
			if readErr != nil {
				return observed, errors.Join(writeErr, readErr)
			}
			current = observed
		}

		desired := slices.Clone(v.Members)
		slices.Sort(desired)
		observed, err := waitLAG(ctx, d, v.ID, func(lag *config) bool {
			return lag != nil && lag.Name == v.Name && lag.Mode == v.Mode && slices.Equal(lag.Members, desired)
		})
		if err != nil {
			return observed, err
		}

		return observed, nil
	})
}

func detachPort(ctx context.Context, d *fastiron.Device, update *fastiron.Update, id int64, name string) error {
	// Native removal disables the detached port. Administrative configuration
	// belongs to the Ethernet resource; do not restore it as a LAG side effect.
	endpoint := path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/config/aggregate-id")
	writeErr := update.REST(http.MethodDelete, endpoint, nil)
	_, readErr := waitLAG(ctx, d, id, func(lag *config) bool { return lag == nil || !slices.Contains(lag.Members, name) })
	if readErr != nil {
		return errors.Join(writeErr, readErr)
	}
	return nil
}

func deleteLAG(ctx context.Context, d *fastiron.Device, id int64) error {
	if id < 1 {
		return errors.New("lag_id must be positive")
	}

	return d.Update(ctx, func(update *fastiron.Update) error {
		current, err := readLAG(ctx, d, id)
		if errors.Is(err, fastiron.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		name := "lag " + strconv.FormatInt(id, 10)
		port, err := vlan.ReadSwitchport(ctx, d, name)
		if err != nil {
			return err
		}
		if port.Access > 1 || len(port.Trunks) > 0 {
			return errors.New("LAG has VLAN memberships; remove them before destroying it")
		}
		document, err := d.RunningConfig(ctx)
		if err != nil {
			return err
		}
		if err := document.CheckLAGRemoval(id, current.Members); err != nil {
			return err
		}
		// FastIron can rebuild default STP references after policy cleanup.
		// The native child guard above confirms there is no policy to erase;
		// remove the reference immediately before its parent, without readback between them.
		if err := update.DeleteIfPresent(path.Join("/stp/interfaces", "interface="+url.PathEscape(name))); err != nil {
			return err
		}
		writeErr := update.REST(http.MethodDelete, path.Join("/interfaces", "interface="+url.PathEscape(name)), nil)
		if errors.Is(writeErr, restconf.ErrNotFound) {
			// RESTCONF can expose a native aggregate it cannot delete. Confirm
			// existence after the request before directing the operator to CLI.
			document, err := d.RunningConfig(ctx)
			if err != nil {
				return err
			}
			present, err := document.HasLAG(id)
			if err != nil {
				return err
			}
			if present {
				return fmt.Errorf("RESTCONF cannot delete native lag %d; remove the LAG through CLI, then retry apply to finish persistence", id)
			}
		}
		_, readErr := waitLAG(ctx, d, id, func(lag *config) bool { return lag == nil })
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}

		return nil
	})
}
