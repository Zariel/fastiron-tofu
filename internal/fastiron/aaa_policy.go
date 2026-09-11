package fastiron

import (
	"context"
	"errors"
	"net/http"
	"slices"
)

type AAAPolicy struct {
	LoginMethods []string
	Dot1XDefault string
	CoAEnabled   bool
	CoAIgnore    []string
}

func (d *Device) AAAPolicy(ctx context.Context) (*AAAPolicy, error) {
	if d.config.Transport == "ssh" || d.rest == nil {
		return nil, errors.New("AAA policy discovery currently requires RESTCONF")
	}
	var response struct {
		AAA *struct {
			Authentication *struct {
				Login *struct {
					Default *[]string `json:"default"`
				} `json:"icx-openconfig-aaa-aug:login"`
				Dot1X *struct {
					Default *string `json:"default"`
				} `json:"icx-openconfig-aaa-aug:dot1x"`
			} `json:"authentication"`
			Authorization *struct {
				CoA *struct {
					Enable *bool            `json:"enable"`
					Ignore map[string]*bool `json:"ignore"`
				} `json:"icx-openconfig-aaa-aug:coa"`
			} `json:"authorization"`
		} `json:"openconfig-system:aaa"`
	}
	if err := d.rest.Do(ctx, http.MethodGet, "/system/aaa", nil, &response); err != nil {
		return nil, err
	}
	if response.AAA == nil || response.AAA.Authentication == nil || response.AAA.Authorization == nil {
		return nil, errors.New("RESTCONF AAA response is missing its policy containers")
	}
	auth, coa := response.AAA.Authentication, response.AAA.Authorization.CoA
	if auth.Login == nil || auth.Login.Default == nil || auth.Dot1X == nil || auth.Dot1X.Default == nil || coa == nil || coa.Enable == nil || coa.Ignore == nil {
		return nil, errors.New("RESTCONF AAA response is missing policy settings")
	}
	for _, action := range []string{"disable-port", "dm-request", "flip-port", "modify-acl", "reauth-host"} {
		if coa.Ignore[action] == nil {
			return nil, errors.New("RESTCONF AAA response is missing a CoA ignore setting")
		}
	}
	policy := &AAAPolicy{LoginMethods: []string{}, Dot1XDefault: *auth.Dot1X.Default, CoAEnabled: *coa.Enable, CoAIgnore: []string{}}
	policy.LoginMethods = append(policy.LoginMethods, (*auth.Login.Default)...)
	for action, ignored := range coa.Ignore {
		if ignored == nil {
			return nil, errors.New("RESTCONF AAA response has a null CoA ignore setting")
		}
		if *ignored {
			policy.CoAIgnore = append(policy.CoAIgnore, action)
		}
	}
	slices.Sort(policy.CoAIgnore)
	return policy, nil
}
