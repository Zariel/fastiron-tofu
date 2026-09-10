package fastiron

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
)

func ValidateLAG(v LAG) error {
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
		if !strings.HasPrefix(name, "ethernet ") || !portPattern.MatchString(strings.TrimPrefix(name, "ethernet ")) {
			return errors.New("LAG members must be canonical Ethernet interface names")
		}
		if seen[name] {
			return errors.New("LAG members must not contain duplicates")
		}
		seen[name] = true
	}
	return nil
}

func (d *Device) LAG(ctx context.Context, id int64) (LAG, error) {
	if id < 1 {
		return LAG{}, errors.New("lag_id must be positive")
	}

	lags, err := d.LAGs(ctx)
	if err != nil {
		return LAG{}, err
	}
	for _, lag := range lags {
		if lag.ID == id {
			return lag, nil
		}
	}
	return LAG{}, ErrNotFound
}

func (d *Device) waitLAG(ctx context.Context, id int64, matches func(*LAG) bool) (*LAG, error) {
	ctx, cancel := context.WithTimeout(ctx, d.config.RESTCONF.Timeout)
	defer cancel()
	for {
		lag, err := d.LAG(ctx, id)
		var current *LAG
		if err == nil {
			current = &lag
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if matches(current) {
			return current, nil
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return current, fmt.Errorf("LAG configuration did not converge: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// ApplyLAG reports observed configuration, including after a failed save.
func (d *Device) ApplyLAG(ctx context.Context, v LAG) (*LAG, error) {
	if err := ValidateLAG(v); err != nil {
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

	lags, err := d.LAGs(ctx)
	if err != nil {
		return nil, err
	}

	var current *LAG
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
	if current != nil && current.Mode != v.Mode {
		return nil, errors.New("changing LAG mode requires replacement")
	}

	for _, name := range v.Members {
		if current != nil && slices.Contains(current.Members, name) {
			continue
		}
		if _, err := d.Ethernet(ctx, strings.TrimPrefix(name, "ethernet ")); err != nil {
			return nil, err
		}
		port, err := d.switchport(ctx, name)
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
		writeErr := d.rest.Do(ctx, method, "/interfaces", body, nil)
		observed, readErr := d.waitLAG(ctx, v.ID, func(lag *LAG) bool { return lag != nil && lag.Name == v.Name && lag.Mode == v.Mode })
		if readErr != nil {
			return observed, errors.Join(writeErr, readErr)
		}
		current = observed
	}

	for _, name := range slices.Clone(current.Members) {
		if slices.Contains(v.Members, name) {
			continue
		}
		if err := d.detachLAGPort(ctx, v.ID, name); err != nil {
			observed, readErr := d.LAG(ctx, v.ID)
			if readErr != nil {
				return nil, errors.Join(err, readErr)
			}
			return &observed, err
		}
	}

	for _, name := range v.Members {
		lag, err := d.LAG(ctx, v.ID)
		if err != nil {
			return current, err
		}
		if slices.Contains(lag.Members, name) {
			continue
		}
		endpoint := path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/config")
		body := map[string]any{"config": map[string]any{"openconfig-if-aggregate:aggregate-id": "lag " + strconv.FormatInt(v.ID, 10)}}
		writeErr := d.rest.Do(ctx, http.MethodPatch, endpoint, body, nil)
		observed, readErr := d.waitLAG(ctx, v.ID, func(lag *LAG) bool { return lag != nil && slices.Contains(lag.Members, name) })
		if readErr != nil {
			return observed, errors.Join(writeErr, readErr)
		}
		current = observed
	}

	desired := slices.Clone(v.Members)
	slices.Sort(desired)
	observed, err := d.waitLAG(ctx, v.ID, func(lag *LAG) bool {
		return lag != nil && lag.Name == v.Name && lag.Mode == v.Mode && slices.Equal(lag.Members, desired)
	})
	if err != nil {
		return observed, err
	}

	if d.config.Persistence == "after_each_write" {
		return observed, d.save(ctx)
	}
	return observed, nil
}

func (d *Device) detachLAGPort(ctx context.Context, id int64, name string) error {
	// Native removal disables the detached port. Administrative configuration
	// belongs to the Ethernet resource; do not restore it as a LAG side effect.
	endpoint := path.Join("/interfaces", "interface="+url.PathEscape(name), "ethernet/config/aggregate-id")
	writeErr := d.rest.Do(ctx, http.MethodDelete, endpoint, nil, nil)
	_, readErr := d.waitLAG(ctx, id, func(lag *LAG) bool { return lag == nil || !slices.Contains(lag.Members, name) })
	if readErr != nil {
		return errors.Join(writeErr, readErr)
	}
	return nil
}

func (d *Device) DeleteLAG(ctx context.Context, id int64) error {
	if id < 1 {
		return errors.New("lag_id must be positive")
	}

	unlock, err := d.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	if _, err := d.Discover(ctx); err != nil {
		return err
	}

	current, err := d.LAG(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil {
		name := "lag " + strconv.FormatInt(id, 10)
		port, err := d.switchport(ctx, name)
		if err != nil {
			return err
		}
		if port.Access > 1 || len(port.Trunks) > 0 {
			return errors.New("LAG has VLAN memberships; remove them before destroying it")
		}
		output, err := d.cli.Run(ctx, true, "show running-config")
		if err != nil {
			return err
		}
		if err := lagChildren(output[0], current); err != nil {
			return err
		}
		writeErr := d.rest.Do(ctx, http.MethodDelete, path.Join("/interfaces", "interface="+url.PathEscape(name)), nil, nil)
		_, readErr := d.waitLAG(ctx, id, func(lag *LAG) bool { return lag == nil })
		if readErr != nil {
			return errors.Join(writeErr, readErr)
		}
	}

	if d.config.Persistence == "after_each_write" {
		return d.save(ctx)
	}
	return nil
}

func lagChildren(config string, lag LAG) error {
	if _, err := configuration(config); err != nil {
		return err
	}

	inside, virtual, found := false, false, false
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimRight(line, " \r\t")
		if strings.HasPrefix(line, "lag ") && strings.HasSuffix(line, " id "+strconv.FormatInt(lag.ID, 10)) {
			inside, virtual, found = true, false, true
			continue
		}
		if line == "interface lag "+strconv.FormatInt(lag.ID, 10) {
			inside, virtual = true, true
			continue
		}
		if !inside {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "!" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			inside = false
			continue
		}
		// The virtual interface is a separate native block and owns independent
		// settings that deleting the aggregate would remove.
		if virtual {
			return errors.New("LAG has independent interface configuration; remove it before destroying the LAG")
		}
		if strings.HasPrefix(trimmed, "ports ") {
			continue
		}
		preserved := false
		for _, member := range lag.Members {
			if trimmed == "disable ethe "+strings.TrimPrefix(member, "ethernet ") || (strings.HasPrefix(trimmed, "port-name ") && strings.HasSuffix(trimmed, " "+member)) {
				preserved = true
				break
			}
		}
		if preserved {
			continue
		}
		return errors.New("LAG has independent interface or protocol configuration; remove it before destroying the LAG")
	}
	if !found {
		return errors.New("cannot confirm the LAG configuration block before deletion")
	}
	return nil
}
