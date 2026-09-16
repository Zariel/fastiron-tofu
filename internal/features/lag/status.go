package lag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type interfaceStatus struct {
	IfIndex                 *uint32
	AdminStatus, OperStatus *string
	Counters                map[string]uint64
}

func readStatus(ctx context.Context, d *fastiron.Device, name string) (interfaceStatus, error) {
	var response struct {
		Interfaces []struct {
			Name   string `json:"name"`
			Config *struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"config"`
			State *struct {
				Name        string                 `json:"name"`
				IfIndex     *uint32                `json:"ifindex"`
				AdminStatus *string                `json:"admin-status"`
				OperStatus  *string                `json:"oper-status"`
				Counters    map[string]json.Number `json:"counters"`
			} `json:"state"`
		} `json:"openconfig-interfaces:interface"`
	}
	endpoint := path.Join("/interfaces", "interface="+url.PathEscape(name))
	if err := d.ReadREST(ctx, endpoint, &response); err != nil {
		return interfaceStatus{}, err
	}
	if len(response.Interfaces) != 1 {
		return interfaceStatus{}, errors.New("RESTCONF LAG status must contain exactly one interface")
	}
	entry := response.Interfaces[0]
	if entry.Name != name || entry.Config == nil || entry.Config.Name != name || entry.Config.Type != "iana-if-type:ieee8023adLag" {
		return interfaceStatus{}, errors.New("RESTCONF LAG status has an invalid interface identity")
	}
	var observed interfaceStatus
	if entry.State == nil {
		return observed, nil
	}
	state := entry.State
	if state.Name != "" && state.Name != name {
		return interfaceStatus{}, errors.New("RESTCONF LAG operational identity disagrees with configuration")
	}
	observed.IfIndex, observed.AdminStatus, observed.OperStatus = state.IfIndex, state.AdminStatus, state.OperStatus
	if state.Counters != nil {
		observed.Counters = make(map[string]uint64, len(state.Counters))
	}
	for name, raw := range state.Counters {
		value, err := strconv.ParseUint(raw.String(), 10, 64)
		if err != nil {
			return interfaceStatus{}, fmt.Errorf("RESTCONF LAG counter %q is not an unsigned 64-bit integer", name)
		}
		observed.Counters[name] = value
	}
	return observed, nil
}
