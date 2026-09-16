package lag

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type interfaceConfig = nativeconfig.LAGInterface

type interfaceObservation struct {
	config   interfaceConfig
	lag      config
	document *nativeconfig.Document
	ports    []string
}

func validateInterface(id int64, desired interfaceConfig) error {
	if id < 1 {
		return errors.New("lag_id must be positive")
	}
	return interfaceid.ValidatePortName(desired.PortName)
}

func readInterface(ctx context.Context, d *fastiron.Device, id int64) (interfaceObservation, error) {
	if id < 1 {
		return interfaceObservation{}, errors.New("lag_id must be positive")
	}
	// Administrative writes affect members, so validate their complete physical inventory.
	cached, err := waitCollection(ctx, d)
	if err != nil {
		return interfaceObservation{}, err
	}
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return interfaceObservation{}, err
	}
	lags, err := document.LAGs(cached.ports)
	if err != nil {
		return interfaceObservation{}, err
	}
	var parent *config
	for _, lag := range lags {
		if lag.ID == id {
			parent = &lag
			break
		}
	}
	if parent == nil {
		return interfaceObservation{}, fastiron.ErrNotFound
	}
	found := false
	for _, lag := range cached.lags {
		if lag.ID == id {
			found = true
			break
		}
	}
	if !found {
		return interfaceObservation{}, errors.New("RESTCONF has not discovered the native LAG identity")
	}
	current, err := document.LAGInterface(id)
	if err != nil {
		return interfaceObservation{}, err
	}
	return interfaceObservation{config: current, lag: *parent, document: document, ports: cached.ports}, nil
}

func waitInterface(ctx context.Context, d *fastiron.Device, id int64, desired interfaceConfig) (interfaceObservation, error) {
	// Retry time is separate from each RESTCONF and SSH operation's deadline.
	retryCtx, cancel := context.WithTimeout(ctx, d.RESTCONFTimeout())
	defer cancel()
	for {
		observed, err := readInterface(ctx, d, id)
		if err != nil || observed.config == desired {
			return observed, err
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return observed, fmt.Errorf("LAG interface configuration did not converge: %w", retryCtx.Err())
		case <-timer.C:
		}
	}
}

func applyInterface(ctx context.Context, d *fastiron.Device, id int64, desired interfaceConfig, present bool) (*interfaceConfig, error) {
	if !present {
		desired = interfaceConfig{Enabled: true}
	}
	if err := validateInterface(id, desired); err != nil {
		return nil, err
	}
	return fastiron.Reconcile(ctx, d, func(update *fastiron.Update) (*interfaceConfig, error) {
		before, err := readInterface(ctx, d, id)
		if errors.Is(err, fastiron.ErrNotFound) && !present {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if before.config == desired {
			return &before.config, nil
		}
		if len(before.lag.Members) == 0 {
			return &before.config, errors.New("LAG interface configuration requires at least one member")
		}

		endpoint := path.Join("/interfaces", "interface="+url.PathEscape("lag "+strconv.FormatInt(id, 10)), "config")
		// Prime with the observed native value before changing a potentially stale cached
		// value. Do not read between priming and mutation: reads can rebuild that cache.
		// Update retains every write error while allowing final native observation.
		var writeErr error
		if before.config.PortName != desired.PortName {
			writeErr = update.REST(http.MethodPatch, endpoint, map[string]any{"config": map[string]any{"description": before.config.PortName}})
			// Empty description resets the native name without the leaf DELETE
			// that can stall FastIron configuration after a reboot.
			writeErr = update.REST(http.MethodPatch, endpoint, map[string]any{"config": map[string]any{"description": desired.PortName}})
		}
		if before.config.Enabled != desired.Enabled {
			if desired.Enabled {
				writeErr = update.DeleteIfPresent(path.Join(endpoint, "enabled"))
			} else {
				writeErr = update.REST(http.MethodPatch, endpoint, map[string]any{"config": map[string]any{"enabled": before.config.Enabled}})
				writeErr = update.REST(http.MethodPatch, endpoint, map[string]any{"config": map[string]any{"enabled": false}})
			}
		}

		var observed interfaceObservation
		if writeErr != nil {
			observed, err = readInterface(ctx, d, id)
		} else {
			observed, err = waitInterface(ctx, d, id, desired)
		}
		if observed.document == nil {
			return &before.config, err
		}
		verifyErr := observed.document.CheckLAGInterfaceUpdate(before.document, id, before.ports)
		return &observed.config, errors.Join(err, verifyErr)
	})
}
