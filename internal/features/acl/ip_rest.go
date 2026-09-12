package acl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

func checkIPSequences(ctx context.Context, d *fastiron.Device, family ipFamily, name string, current *ipConfig) error {
	type entry struct {
		Sequence int64 `json:"sequence-id"`
	}
	type entries struct {
		Rules []entry `json:"acl-entry"`
	}
	type set struct {
		Name    string  `json:"name"`
		Type    string  `json:"type"`
		Entries entries `json:"acl-entries"`
	}
	type collection struct {
		Sets []set `json:"acl-set"`
	}
	var response struct {
		ACLs *collection `json:"openconfig-acl:acl-sets"`
	}
	if err := d.DoREST(ctx, http.MethodGet, aclPath, nil, &response); err != nil {
		return err
	}
	if response.ACLs == nil {
		return errors.New("RESTCONF response omitted the ACL collection")
	}

	found := false
	sequences := map[int64]bool{}
	for _, acl := range response.ACLs.Sets {
		if acl.Name != name || strings.TrimPrefix(acl.Type, "openconfig-acl:") != family.restType() {
			continue
		}
		if found {
			return errors.New("duplicate RESTCONF ACL identity")
		}
		found = true
		for _, rule := range acl.Entries.Rules {
			if rule.Sequence < 1 || rule.Sequence > 65000 || sequences[rule.Sequence] {
				return errors.New("invalid or duplicate RESTCONF ACL sequence")
			}
			sequences[rule.Sequence] = true
		}
	}
	matches := found == (current != nil)
	if current != nil {
		matches = found && len(sequences) == len(current.Rules)
		for sequence := range current.Rules {
			if !sequences[sequence] {
				matches = false
				break
			}
		}
	}
	if !matches {
		return fmt.Errorf("RESTCONF and native rule sequences disagree for %s; synchronize RESTCONF after CLI changes and retry", family.header(name))
	}
	return nil
}
