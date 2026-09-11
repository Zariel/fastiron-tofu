package fastiron

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
)

type AAAServer struct {
	Kind, Address, Purpose string
	AuthPort, AcctPort     int64
}

func (d *Device) AAAServers(ctx context.Context) ([]AAAServer, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("AAA server discovery currently requires RESTCONF")
	}
	type serverConfig struct {
		AuthPort *int64 `json:"auth-port"`
		Port     *int64 `json:"port"`
		AcctPort *int64 `json:"acct-port"`
		Purpose  string `json:"icx-openconfig-aaa-aug:purpose"`
	}
	type serverProtocol struct {
		Config *serverConfig `json:"config"`
	}
	var response struct {
		Groups *struct {
			Group []struct {
				Name   string `json:"name"`
				Config *struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"config"`
				Servers *struct {
					Server []struct {
						Address string `json:"address"`
						Config  *struct {
							Address string `json:"address"`
						} `json:"config"`
						Radius *serverProtocol `json:"radius"`
						Tacacs *serverProtocol `json:"tacacs"`
					} `json:"server"`
				} `json:"servers"`
			} `json:"server-group"`
		} `json:"openconfig-system:server-groups"`
	}
	if err := d.rest.Do(ctx, http.MethodGet, "/system/aaa/server-groups", nil, &response); err != nil {
		return nil, err
	}
	if response.Groups == nil {
		return nil, errors.New("RESTCONF AAA response is missing its server-group container")
	}
	servers := []AAAServer{}
	groups := map[string]bool{}
	for _, group := range response.Groups.Group {
		kind := ""
		switch group.Name {
		case "radius-default-group":
			kind = "radius"
		case "tacacs-default-group":
			kind = "tacacs"
		default:
			return nil, errors.New("RESTCONF AAA response has an unsupported server group")
		}
		if groups[group.Name] || group.Config == nil || group.Config.Name != group.Name || strings.TrimPrefix(group.Config.Type, "openconfig-aaa:") != strings.ToUpper(kind) {
			return nil, errors.New("RESTCONF AAA response has an inconsistent or duplicate group identity")
		}
		groups[group.Name] = true
		if group.Servers == nil {
			return nil, errors.New("RESTCONF AAA group is missing its server collection")
		}
		addresses := map[string]bool{}
		for _, entry := range group.Servers.Server {
			if entry.Address == "" || entry.Config == nil || entry.Config.Address != entry.Address || addresses[entry.Address] {
				return nil, errors.New("RESTCONF AAA response has an inconsistent or duplicate server identity")
			}
			addresses[entry.Address] = true
			protocol := entry.Radius
			if kind == "tacacs" {
				protocol = entry.Tacacs
			}
			if protocol == nil || protocol.Config == nil {
				return nil, errors.New("RESTCONF AAA server is missing its protocol configuration")
			}
			c := protocol.Config
			port := c.AuthPort
			if kind == "tacacs" {
				port = c.Port
			}
			if port == nil || *port < 1 || *port > 65535 {
				return nil, errors.New("RESTCONF AAA server has an invalid authentication port")
			}
			server := AAAServer{Kind: kind, Address: entry.Address, AuthPort: *port, Purpose: c.Purpose}
			if kind == "radius" {
				if c.AcctPort == nil || *c.AcctPort < 1 || *c.AcctPort > 65535 {
					return nil, errors.New("RESTCONF RADIUS server has an invalid accounting port")
				}
				server.AcctPort = *c.AcctPort
			}
			if c.Purpose != "default" && c.Purpose != "accounting-only" && c.Purpose != "authentication-only" && !(kind == "tacacs" && c.Purpose == "authorization-only") {
				return nil, errors.New("RESTCONF AAA server has an unsupported purpose")
			}
			servers = append(servers, server)
		}
	}
	slices.SortFunc(servers, func(a, b AAAServer) int {
		if n := strings.Compare(a.Kind, b.Kind); n != 0 {
			return n
		}
		return strings.Compare(a.Address, b.Address)
	})
	return servers, nil
}
