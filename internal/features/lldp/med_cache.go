package lldp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"

	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type medAttachment struct {
	Interface   string
	Application string
	Policy      config.MEDPolicy
}

func (a medAttachment) path() string {
	p := a.Policy
	key := fmt.Sprint(p.DSCP)
	if p.Traffic != "untagged" {
		key = fmt.Sprintf("%d,%d", p.Priority, p.DSCP)
	}
	if p.Traffic == "tagged" {
		key = fmt.Sprintf("%d,%s", p.VLAN, key)
	}
	return path.Join("/lldp/med", "network-policy="+a.Application+","+p.Traffic, p.Traffic+"="+key, "ports="+url.PathEscape(a.Interface))
}

func validMEDApplication(application string) bool {
	switch application {
	case "voice", "voice-signaling", "guest-voice", "guest-voice-signaling", "softphone-voice", "streaming-video", "video-conferencing", "video-signaling":
		return true
	}
	return false
}

func readMEDCache(ctx context.Context, device *fastiron.Device) ([]medAttachment, error) {
	if !device.RESTCONFEnabled() {
		return nil, errors.New("LLDP-MED currently requires RESTCONF")
	}
	var response struct {
		MED json.RawMessage `json:"icx-openconfig-lldp-aug:med"`
	}
	if err := device.ReadREST(ctx, "/lldp/med", &response); err != nil {
		return nil, err
	}
	return decodeMEDCache(response.MED)
}

func decodeMEDCache(raw json.RawMessage) ([]medAttachment, error) {
	var shape any
	if err := json.Unmarshal(raw, &shape); err != nil {
		return nil, errors.New("RESTCONF LLDP-MED response is missing its configuration container")
	}
	if values, ok := shape.([]any); ok && len(values) == 1 && values[0] == nil {
		// This sentinel does not prove native absence after CLI changes.
		return nil, nil
	}
	if _, ok := shape.(map[string]any); !ok {
		return nil, errors.New("RESTCONF LLDP-MED configuration container is malformed")
	}
	var container struct {
		Policies []struct {
			Application    string          `json:"application"`
			Traffic        string          `json:"traffic"`
			Tagged         []medCacheEntry `json:"tagged"`
			PriorityTagged []medCacheEntry `json:"priority-tagged"`
			Untagged       []medCacheEntry `json:"untagged"`
		} `json:"network-policy"`
	}
	if err := json.Unmarshal(raw, &container); err != nil {
		return nil, err
	}
	var attachments []medAttachment
	seen := map[medAttachment]bool{}
	for _, group := range container.Policies {
		if !validMEDApplication(group.Application) {
			return nil, errors.New("RESTCONF MED application is invalid")
		}
		var entries []medCacheEntry
		switch group.Traffic {
		case "tagged":
			entries = group.Tagged
		case "priority-tagged":
			entries = group.PriorityTagged
		case "untagged":
			entries = group.Untagged
		default:
			return nil, errors.New("RESTCONF MED traffic mode is invalid")
		}
		if len(entries) != len(group.Tagged)+len(group.PriorityTagged)+len(group.Untagged) {
			return nil, errors.New("RESTCONF MED traffic mode and policy entries disagree")
		}
		for _, e := range entries {
			p, err := e.policy(group.Traffic)
			if err != nil {
				return nil, err
			}
			// Empty groups can remain after the last port is removed; they own no port.
			for _, name := range e.Ports {
				if err := validateInterface(name); err != nil {
					return nil, err
				}
				a := medAttachment{Interface: name, Application: group.Application, Policy: p}
				if seen[a] {
					return nil, errors.New("RESTCONF MED port attachment is repeated")
				}
				seen[a] = true
				attachments = append(attachments, a)
			}
		}
	}
	return attachments, nil
}

type medCacheEntry struct {
	VLAN     *int64   `json:"vlan"`
	Priority *int64   `json:"priority"`
	DSCP     *int64   `json:"dscp"`
	Ports    []string `json:"ports"`
}

func (e medCacheEntry) policy(traffic string) (config.MEDPolicy, error) {
	p := config.MEDPolicy{Traffic: traffic}
	if e.DSCP == nil || *e.DSCP < 0 || *e.DSCP > 63 {
		return config.MEDPolicy{}, errors.New("RESTCONF MED DSCP is missing or invalid")
	}
	p.DSCP = *e.DSCP
	if p.Traffic != "untagged" {
		if e.Priority == nil || *e.Priority < 0 || *e.Priority > 7 {
			return config.MEDPolicy{}, errors.New("RESTCONF MED priority is missing or invalid")
		}
		p.Priority = *e.Priority
	} else if e.Priority != nil {
		return config.MEDPolicy{}, errors.New("RESTCONF untagged MED policy contains a priority")
	}
	if p.Traffic == "tagged" {
		if e.VLAN == nil || *e.VLAN < 1 || *e.VLAN > 4094 {
			return config.MEDPolicy{}, errors.New("RESTCONF MED VLAN is missing or invalid")
		}
		p.VLAN = *e.VLAN
	} else if e.VLAN != nil {
		return config.MEDPolicy{}, errors.New("RESTCONF untagged or priority-tagged MED policy contains a VLAN")
	}

	return p, nil
}
