package fastiron

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/interfaceid"
)

type l2Entry struct {
	Name   string `json:"name"`
	Config *struct {
		Name string `json:"name"`
	} `json:"config"`
	Ethernet *struct {
		Config *struct {
			Aggregate string `json:"openconfig-if-aggregate:aggregate-id"`
		} `json:"config"`
	} `json:"openconfig-if-ethernet:ethernet"`
}

// L2Config returns one native snapshot with the selected Layer 2 owner validated.
// Physical-port policies such as PoE have different ownership and must not use it.
func (d *Device) L2Config(ctx context.Context, name string) (*config.Document, error) {
	var response struct {
		Interfaces *struct {
			Entries []l2Entry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.ReadREST(ctx, "/interfaces", &response); err != nil {
		return nil, err
	}
	if response.Interfaces == nil || len(response.Interfaces.Entries) == 0 {
		return nil, errors.New("RESTCONF interface collection is missing or empty")
	}

	found := false
	for _, entry := range response.Interfaces.Entries {
		if entry.Name != name {
			continue
		}
		if found || entry.Config == nil || entry.Config.Name != name {
			return nil, errors.New("RESTCONF interface parent identity is inconsistent")
		}
		if entry.Ethernet != nil && entry.Ethernet.Config != nil && entry.Ethernet.Config.Aggregate != "" {
			return nil, fmt.Errorf("%s is a LAG member; manage or query this policy on %s", name, entry.Ethernet.Config.Aggregate)
		}
		found = true
	}

	isLAG := interfaceid.LAG(name)
	if !found && !isLAG {
		return nil, ErrNotFound
	}
	document, err := d.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	if isLAG {
		// RESTCONF can retain a deleted aggregate after native settings move to its members.
		id, err := strconv.ParseInt(strings.TrimPrefix(name, "lag "), 10, 64)
		if err != nil {
			return nil, err
		}
		exists, err := document.HasLAG(id)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		if !found {
			return nil, errors.New("RESTCONF has not discovered the native LAG identity")
		}
	}
	return document, nil
}
