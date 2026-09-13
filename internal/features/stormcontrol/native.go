package stormcontrol

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
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
	configuration, err := fastiron.NormalizeConfiguration(configuration)
	if err != nil {
		return nativeState{}, err
	}
	state := nativeState{policy: policy{limits: map[string]int64{}}}
	inside, found := false, false
	for _, line := range strings.Split(configuration, "\n") {
		if line[0] != ' ' && line[0] != '\t' {
			inside = line == "interface "+name
			if inside && found {
				return nativeState{}, errors.New("native configuration repeats the requested interface")
			}
			if inside {
				found = true
				// An otherwise default interface can gain or lose its stanza with its policy.
				continue
			}
		}
		fields := strings.Fields(line)
		if !inside || len(fields) < 2 || !slices.Contains(classes, fields[0]) || fields[1] != "limit" {
			state.unowned = append(state.unowned, line)
			continue
		}
		if len(fields) < 3 {
			return nativeState{}, errors.New("native storm limit is missing its rate")
		}
		rate, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || rate <= 0 {
			return nativeState{}, errors.New("native storm limit is invalid")
		}
		if _, duplicate := state.policy.limits[fields[0]]; duplicate {
			return nativeState{}, errors.New("native configuration repeats a storm traffic class")
		}
		unit, options := "pps", fields[3:]
		if len(options) > 0 && (options[0] == "kbps" || options[0] == "pps") {
			unit, options = options[0], options[1:]
		}
		if len(options) > 0 && options[0] != "log" && options[0] != "threshold" {
			return nativeState{}, fmt.Errorf("unsupported native storm option %q", options[0])
		}
		if state.policy.unit != "" && state.policy.unit != unit {
			return nativeState{}, errors.New("native storm limits use different units on one interface")
		}
		state.policy.unit = unit
		state.policy.limits[fields[0]] = rate
		state.options = state.options || len(options) != 0
	}
	return state, nil
}

func (s nativeState) writable() error {
	if s.options {
		return errors.New("native storm logging, thresholds or shutdown actions cannot be preserved by RESTCONF rate updates; remove those options before managing this policy")
	}
	return nil
}
