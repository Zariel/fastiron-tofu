package lag

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	nativeconfig "github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type config = nativeconfig.LAG

type collection struct {
	lags  []config
	ports []string
}

var errLAGSync = errors.New("RESTCONF LAG membership has not synchronized")

func readLAGs(ctx context.Context, d *fastiron.Device) ([]config, error) {
	if !d.RESTCONFEnabled() {
		return nil, errors.New("LAG discovery currently requires RESTCONF")
	}
	cached, err := waitCollection(ctx, d)
	if err != nil {
		return nil, err
	}
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	return document.LAGs(cached.ports)
}

func waitCollection(ctx context.Context, d *fastiron.Device) (collection, error) {
	ctx, cancel := context.WithTimeout(ctx, d.RESTCONFTimeout())
	defer cancel()
	for {
		cached, err := readCollection(ctx, d)
		if !errors.Is(err, errLAGSync) {
			return cached, err
		}
		// Native deletion can remove the aggregate before member references clear.
		// Retry only reads of this known inconsistent state, never the mutation.
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return collection{}, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

func readCollection(ctx context.Context, d *fastiron.Device) (collection, error) {
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
	if err := d.ReadREST(ctx, "/interfaces", &response); err != nil {
		return collection{}, err
	}
	if response.Interfaces == nil {
		return collection{}, errors.New("RESTCONF interface collection is missing its container")
	}
	// Physical interfaces remain present even when no LAGs are configured.
	// FastIron can return an empty container while its database is rebuilding.
	if len(response.Interfaces.Interface) == 0 {
		return collection{}, errors.New("RESTCONF interface collection is empty; cannot confirm LAG state")
	}
	lags := map[string]config{}
	members := map[string][]string{}
	seen := map[string]bool{}
	var ports []string
	for _, entry := range response.Interfaces.Interface {
		if seen[entry.Name] {
			return collection{}, errors.New("RESTCONF interface collection contains duplicate identities")
		}
		seen[entry.Name] = true
		if strings.HasPrefix(entry.Name, "ethernet ") {
			if !interfaceid.EthernetPort(strings.TrimPrefix(entry.Name, "ethernet ")) || entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Type != "iana-if-type:ethernetCsmacd" {
				return collection{}, errors.New("RESTCONF Ethernet inventory contains an invalid identity")
			}
			ports = append(ports, entry.Name)
		}
		if strings.HasPrefix(entry.Name, "lag ") {
			id, err := strconv.ParseInt(strings.TrimPrefix(entry.Name, "lag "), 10, 64)
			if err != nil || id < 1 || entry.Name != "lag "+strconv.FormatInt(id, 10) || entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Type != "iana-if-type:ieee8023adLag" || entry.Aggregation == nil || entry.Aggregation.Config == nil {
				return collection{}, errors.New("RESTCONF LAG response is missing its identity or configuration")
			}
			aggregate := entry.Aggregation.Config
			mode := ""
			switch aggregate.Type {
			case "LACP":
				mode = "dynamic"
			case "STATIC":
				mode = "static"
			default:
				return collection{}, fmt.Errorf("unsupported LAG type %q", aggregate.Type)
			}
			if aggregate.Name == "" {
				return collection{}, errors.New("RESTCONF LAG response is missing its name")
			}
			lags[entry.Name] = config{ID: id, Name: aggregate.Name, Mode: mode, Members: []string{}}
		}
		if entry.Ethernet != nil && entry.Ethernet.Config != nil && entry.Ethernet.Config.Aggregate != "" {
			if !strings.HasPrefix(entry.Name, "ethernet ") || !interfaceid.EthernetPort(strings.TrimPrefix(entry.Name, "ethernet ")) || entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Type != "iana-if-type:ethernetCsmacd" {
				return collection{}, errors.New("RESTCONF LAG member has an invalid Ethernet identity")
			}
			aggregate := entry.Ethernet.Config.Aggregate
			members[aggregate] = append(members[aggregate], entry.Name)
		}
	}
	for name, ports := range members {
		lag, exists := lags[name]
		if !exists {
			return collection{}, fmt.Errorf("%w: member references missing LAG %q", errLAGSync, name)
		}
		sort.Strings(ports)
		lag.Members = ports
		lags[name] = lag
	}
	result := make([]config, 0, len(lags))
	for _, lag := range lags {
		result = append(result, lag)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return collection{lags: result, ports: ports}, nil
}
