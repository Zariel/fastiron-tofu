package fastiron

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LAG struct {
	ID         int64
	Name, Mode string
	Members    []string
}

var errLAGSync = errors.New("RESTCONF LAG membership has not synchronized")

func (d *Device) LAGs(ctx context.Context) ([]LAG, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("LAG discovery currently requires RESTCONF")
	}
	ctx, cancel := context.WithTimeout(ctx, d.config.RESTCONF.Timeout)
	defer cancel()
	for {
		lags, err := d.readLAGs(ctx)
		if !errors.Is(err, errLAGSync) {
			return lags, err
		}
		// Native deletion can remove the aggregate before member references clear.
		// Retry only reads of this known inconsistent state, never the mutation.
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

func (d *Device) readLAGs(ctx context.Context) ([]LAG, error) {
	var response struct {
		Interfaces *struct {
			Interface []struct {
				Name   string `json:"name"`
				Config *struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"config"`
				Aggregation *struct {
					Config *struct {
						Type string `json:"lag-type"`
						Name string `json:"openconfig-if-aggregate-aug:lag-name"`
					} `json:"config"`
				} `json:"openconfig-if-aggregate:aggregation"`
				Ethernet *struct {
					Config *struct {
						Aggregate string `json:"openconfig-if-aggregate:aggregate-id"`
					} `json:"config"`
				} `json:"openconfig-if-ethernet:ethernet"`
			} `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.rest.Do(ctx, http.MethodGet, "/interfaces", nil, &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil {
		return nil, errors.New("RESTCONF interface collection is missing its container")
	}
	lags := map[string]LAG{}
	members := map[string][]string{}
	seen := map[string]bool{}
	for _, entry := range response.Interfaces.Interface {
		if seen[entry.Name] {
			return nil, errors.New("RESTCONF interface collection contains duplicate identities")
		}
		seen[entry.Name] = true
		if strings.HasPrefix(entry.Name, "lag ") {
			id, err := strconv.ParseInt(strings.TrimPrefix(entry.Name, "lag "), 10, 64)
			if err != nil || id < 1 || entry.Name != "lag "+strconv.FormatInt(id, 10) || entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Type != "iana-if-type:ieee8023adLag" || entry.Aggregation == nil || entry.Aggregation.Config == nil {
				return nil, errors.New("RESTCONF LAG response is missing its identity or configuration")
			}
			config := entry.Aggregation.Config
			mode := ""
			switch config.Type {
			case "LACP":
				mode = "dynamic"
			case "STATIC":
				mode = "static"
			default:
				return nil, fmt.Errorf("unsupported LAG type %q", config.Type)
			}
			if config.Name == "" {
				return nil, errors.New("RESTCONF LAG response is missing its name")
			}
			lags[entry.Name] = LAG{ID: id, Name: config.Name, Mode: mode, Members: []string{}}
		}
		if entry.Ethernet != nil && entry.Ethernet.Config != nil && entry.Ethernet.Config.Aggregate != "" {
			if !strings.HasPrefix(entry.Name, "ethernet ") || !portPattern.MatchString(strings.TrimPrefix(entry.Name, "ethernet ")) || entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Type != "iana-if-type:ethernetCsmacd" {
				return nil, errors.New("RESTCONF LAG member has an invalid Ethernet identity")
			}
			aggregate := entry.Ethernet.Config.Aggregate
			members[aggregate] = append(members[aggregate], entry.Name)
		}
	}
	for name, ports := range members {
		lag, exists := lags[name]
		if !exists {
			return nil, fmt.Errorf("%w: member references missing LAG %q", errLAGSync, name)
		}
		sort.Strings(ports)
		lag.Members = ports
		lags[name] = lag
	}
	result := make([]LAG, 0, len(lags))
	for _, lag := range lags {
		result = append(result, lag)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
