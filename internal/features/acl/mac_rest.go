package acl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type macRESTEntry struct {
	ID     int64 `json:"sequence-id"`
	Config struct {
		ID *int64 `json:"sequence-id"`
	} `json:"config"`
	L2 struct {
		Config struct {
			Source          string `json:"source-mac"`
			SourceMask      string `json:"source-mac-mask"`
			Destination     string `json:"destination-mac"`
			DestinationMask string `json:"destination-mac-mask"`
			EtherType       *int64 `json:"ethertype"`
		} `json:"config"`
	} `json:"l2"`
	Actions struct {
		Config struct {
			Forward string `json:"forwarding-action"`
			Log     string `json:"log-action"`
		} `json:"config"`
	} `json:"actions"`
}

func (entry macRESTEntry) rule() (macRule, error) {
	var rule macRule
	if entry.ID < 1 || entry.ID > 65000 || entry.Config.ID == nil || *entry.Config.ID != entry.ID {
		return rule, errors.New("invalid or inconsistent MAC ACL REST entry ID")
	}
	switch strings.TrimPrefix(entry.Actions.Config.Forward, "openconfig-acl:") {
	case "ACCEPT":
		rule.Action = "permit"
	case "DROP":
		rule.Action = "deny"
	default:
		return rule, errors.New("unrecognized MAC ACL REST forwarding action")
	}
	switch strings.TrimPrefix(entry.Actions.Config.Log, "openconfig-acl:") {
	case "", "LOG_NONE":
	case "LOG_SYSLOG":
		rule.Log = true
	default:
		return rule, errors.New("unrecognized MAC ACL REST logging action")
	}
	match := entry.L2.Config
	var err error
	rule.Source, err = parseMACMatch(match.Source, match.SourceMask)
	if err != nil {
		return rule, err
	}
	rule.Destination, err = parseMACMatch(match.Destination, match.DestinationMask)
	if err != nil {
		return rule, err
	}
	if match.EtherType != nil {
		rule.EtherType = optionalInt{Value: *match.EtherType, Present: true}
	}
	return rule, nil
}

func macEntries(ctx context.Context, device *fastiron.Device, name string, current *macConfig) ([]macEntry, error) {
	type aclSet struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Entries struct {
			Rules []macRESTEntry `json:"acl-entry"`
		} `json:"acl-entries"`
	}
	var response struct {
		ACLs *struct {
			Sets []aclSet `json:"acl-set"`
		} `json:"openconfig-acl:acl-sets"`
	}
	if err := device.DoREST(ctx, http.MethodGet, aclPath, nil, &response); err != nil {
		return nil, err
	}
	if response.ACLs == nil {
		return nil, errors.New("RESTCONF response omitted the ACL collection")
	}
	var restEntries []macRESTEntry
	found := false
	for _, acl := range response.ACLs.Sets {
		if acl.Name != name || strings.TrimPrefix(acl.Type, "openconfig-acl:") != "ACL_L2" {
			continue
		}
		if found {
			return nil, errors.New("duplicate MAC ACL REST identity")
		}
		found = true
		restEntries = acl.Entries.Rules
	}

	if found != (current != nil) || (current != nil && len(restEntries) != len(current.Rules)) {
		return nil, fmt.Errorf("RESTCONF and native rules disagree for mac access-list %s; synchronize RESTCONF after CLI changes and retry", name)
	}
	slices.SortFunc(restEntries, func(a, b macRESTEntry) int { return cmp.Compare(a.ID, b.ID) })
	entries := make([]macEntry, 0, len(restEntries))
	for i, entry := range restEntries {
		rule, err := entry.rule()
		if err != nil {
			return nil, err
		}
		if i > 0 && entry.ID == restEntries[i-1].ID {
			return nil, errors.New("duplicate MAC ACL REST entry ID")
		}
		// Reload can omit REST logging metadata. Native configuration retains
		// it, while ascending REST IDs retain native rule order.
		if entry.Actions.Config.Log == "" {
			rule.Log = current.Rules[i].Log
		}
		if rule != current.Rules[i] {
			return nil, fmt.Errorf("RESTCONF and native rules disagree for mac access-list %s; synchronize RESTCONF after CLI changes and retry", name)
		}
		entries = append(entries, macEntry{ID: entry.ID, Rule: rule})
	}
	return entries, nil
}
