package stormcontrol

import (
	"errors"
	"maps"

	"github.com/zariel/fastiron-tofu/internal/config"
)

var classes = []string{"broadcast", "multicast", "unknown-unicast"}

type policy struct {
	unit   string
	limits map[string]int64
}

func (p policy) equal(other policy) bool {
	return p.unit == other.unit && maps.Equal(p.limits, other.limits)
}

type nativeState struct {
	policy  policy
	options bool
	unowned []string
}

func parse(configuration, name string) (nativeState, error) {
	document, err := config.Parse(configuration)
	if err != nil {
		return nativeState{}, err
	}
	observed, err := document.InterfacePolicy(name, config.StormControl)
	if err != nil {
		return nativeState{}, err
	}
	return nativeState{policy: policy{unit: observed.Unit, limits: observed.Limits}, options: observed.Options, unowned: observed.Remaining}, nil
}

func (s nativeState) writable() error {
	if s.options {
		return errors.New("native storm logging, thresholds or shutdown actions cannot be preserved by RESTCONF rate updates; remove those options before managing this policy")
	}
	return nil
}
