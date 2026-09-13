package ethernet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type observation struct {
	config
	IfIndex                 *uint32
	AdminStatus, OperStatus *string
	Counters                map[string]uint64
	Link                    *linkState
}

type linkState struct {
	AutoNegotiate                                      *bool
	Duplex, Speed, Clock                               *string
	ReportedAutoNegotiate                              *bool
	ReportedDuplex                                     *string
	NegotiatedDuplex, NegotiatedSpeed, NegotiatedClock *string
}

type interfaceEntry struct {
	Name   string `json:"name"`
	Config *struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Enabled     *bool   `json:"enabled"`
	} `json:"config"`
	State *struct {
		Name        string                 `json:"name"`
		Description *string                `json:"description"`
		Enabled     *bool                  `json:"enabled"`
		IfIndex     *uint32                `json:"ifindex"`
		AdminStatus *string                `json:"admin-status"`
		OperStatus  *string                `json:"oper-status"`
		Counters    map[string]json.Number `json:"counters"`
	} `json:"state"`
	Ethernet *struct {
		Config *struct {
			AutoNegotiate *bool   `json:"auto-negotiate"`
			Duplex        *string `json:"duplex-mode"`
			Speed         *string `json:"port-speed"`
			Clock         *string `json:"icx-openconfig-if-ethernet-aug:ethernet-clock"`
		} `json:"config"`
		State *struct {
			AutoNegotiate    *bool   `json:"auto-negotiate"`
			Duplex           *string `json:"duplex-mode"`
			NegotiatedDuplex *string `json:"negotiated-duplex-mode"`
			NegotiatedSpeed  *string `json:"negotiated-port-speed"`
			NegotiatedClock  *string `json:"icx-openconfig-if-ethernet-aug:negotiated-clock"`
		} `json:"state"`
	} `json:"openconfig-if-ethernet:ethernet"`
}

func readObservations(ctx context.Context, device *fastiron.Device) ([]observation, error) {
	var response struct {
		Interfaces *struct {
			Interface []interfaceEntry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := device.DoREST(ctx, http.MethodGet, "/interfaces", nil, &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil || len(response.Interfaces.Interface) == 0 {
		return nil, errors.New("RESTCONF interface collection is missing or empty; cannot confirm Ethernet inventory")
	}
	ports := []observation{}
	seen := map[string]bool{}
	for _, entry := range response.Interfaces.Interface {
		if !strings.HasPrefix(entry.Name, "ethernet ") {
			continue
		}
		port := strings.TrimPrefix(entry.Name, "ethernet ")
		if !interfaceid.EthernetPort(port) || seen[port] {
			return nil, errors.New("RESTCONF Ethernet inventory contains an invalid or duplicate identity")
		}
		seen[port] = true
		if entry.Config == nil || entry.Config.Name != entry.Name || entry.Config.Description == nil || entry.Config.Enabled == nil {
			return nil, errors.New("RESTCONF Ethernet inventory omitted configured identity, description or enable state")
		}
		// The config projection can retain a deleted description after a CLI restore.
		// The state projection reports the native administrative configuration.
		if entry.State == nil || entry.State.Description == nil || entry.State.Enabled == nil {
			return nil, errors.New("RESTCONF Ethernet inventory omitted native description or enable state")
		}
		observed := observation{config: config{Port: port, PortName: *entry.State.Description, Enabled: *entry.State.Enabled}}
		state := entry.State
		if state.Name != "" && state.Name != entry.Name {
			return nil, errors.New("RESTCONF Ethernet operational identity disagrees with configuration")
		}
		observed.IfIndex, observed.AdminStatus, observed.OperStatus = state.IfIndex, state.AdminStatus, state.OperStatus
		if state.Counters != nil {
			observed.Counters = make(map[string]uint64, len(state.Counters))
		}
		for name, raw := range state.Counters {
			value, err := strconv.ParseUint(raw.String(), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("RESTCONF Ethernet counter %q is not an unsigned 64-bit integer", name)
			}
			observed.Counters[name] = value
		}
		if ethernet := entry.Ethernet; ethernet != nil {
			observed.Link = &linkState{}
			if config := ethernet.Config; config != nil {
				observed.Link.AutoNegotiate = config.AutoNegotiate
				observed.Link.Duplex, observed.Link.Speed, observed.Link.Clock = config.Duplex, config.Speed, config.Clock
			}
			if state := ethernet.State; state != nil {
				observed.Link.ReportedAutoNegotiate, observed.Link.ReportedDuplex = state.AutoNegotiate, state.Duplex
				observed.Link.NegotiatedDuplex, observed.Link.NegotiatedSpeed, observed.Link.NegotiatedClock = state.NegotiatedDuplex, state.NegotiatedSpeed, state.NegotiatedClock
			}
		}
		ports = append(ports, observed)
	}
	if len(ports) == 0 {
		return nil, errors.New("RESTCONF interface collection contains no physical Ethernet interfaces")
	}
	return ports, nil
}
