package fastiron

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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

// CheckL2Owner confirms interface identity and rejects Ethernet members whose
// shared Layer 2 policy belongs to a LAG. Physical-port policies such as PoE
// have different ownership and must not use this check.
func (d *Device) CheckL2Owner(ctx context.Context, name string) error {
	var response struct {
		Interfaces *struct {
			Entries []l2Entry `json:"interface"`
		} `json:"openconfig-interfaces:interfaces"`
	}
	if err := d.ReadREST(ctx, "/interfaces", &response); err != nil {
		return err
	}
	if response.Interfaces == nil || len(response.Interfaces.Entries) == 0 {
		return errors.New("RESTCONF interface collection is missing or empty")
	}

	found := false
	for _, entry := range response.Interfaces.Entries {
		if entry.Name != name {
			continue
		}
		if found || entry.Config == nil || entry.Config.Name != name {
			return errors.New("RESTCONF interface parent identity is inconsistent")
		}
		if entry.Ethernet != nil && entry.Ethernet.Config != nil && entry.Ethernet.Config.Aggregate != "" {
			return fmt.Errorf("%s is a LAG member; manage or query this policy on %s", name, entry.Ethernet.Config.Aggregate)
		}
		found = true
	}

	if interfaceid.LAG(name) {
		// RESTCONF can retain a deleted aggregate after native settings move to its members.
		document, err := d.RunningConfig(ctx)
		if err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(name, "lag "), 10, 64)
		if err != nil {
			return err
		}
		exists, err := document.HasLAG(id)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if !found {
			return errors.New("RESTCONF has not discovered the native LAG identity")
		}
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
