package lldp

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func readMED(ctx context.Context, device *fastiron.Device) (map[string]map[string]config.MEDPolicy, []string, error) {
	if !device.RESTCONFEnabled() {
		return nil, nil, errors.New("LLDP-MED currently requires RESTCONF")
	}
	var response struct {
		MED json.RawMessage `json:"icx-openconfig-lldp-aug:med"`
	}
	if err := device.DoREST(ctx, http.MethodGet, "/lldp/med", nil, &response); err != nil {
		return nil, nil, err
	}
	var container any
	if err := json.Unmarshal(response.MED, &container); err != nil {
		return nil, nil, errors.New("RESTCONF LLDP-MED response is missing its configuration container")
	}
	switch value := container.(type) {
	case map[string]any:
	case []any:
		// FastIron can emit [null] even when CLI-created native policies exist.
		if len(value) != 1 || value[0] != nil {
			return nil, nil, errors.New("RESTCONF LLDP-MED configuration container is malformed")
		}
	default:
		return nil, nil, errors.New("RESTCONF LLDP-MED configuration container is malformed")
	}
	inventory, err := readRESTInterfaces(ctx, device)
	if err != nil {
		return nil, nil, err
	}
	raw, err := device.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	document, err := config.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	return document.MEDPolicies(slices.Sorted(maps.Keys(inventory)))
}
