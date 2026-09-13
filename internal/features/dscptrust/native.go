package dscptrust

import (
	"errors"
	"strings"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type nativeState struct {
	enabled              bool
	symmetricFlowControl bool
	unowned              []string
}

func parse(configuration, name string) (nativeState, error) {
	configuration, err := fastiron.NormalizeConfiguration(configuration)
	if err != nil {
		return nativeState{}, err
	}
	var state nativeState
	inside, found := false, false
	for _, line := range strings.Split(configuration, "\n") {
		if line[0] != ' ' && line[0] != '\t' {
			inside = line == "interface "+name
			if inside && found {
				return nativeState{}, errors.New("native configuration repeats the requested interface")
			}
			if inside {
				found = true
				// A default interface can gain or lose its stanza with the trust setting.
				continue
			}
			if strings.HasPrefix(line, "symmetrical-flow-control ") {
				state.symmetricFlowControl = true
			}
		}
		fields := strings.Fields(line)
		if !inside || len(fields) < 2 || fields[0] != "trust" || fields[1] != "dscp" {
			state.unowned = append(state.unowned, line)
			continue
		}
		if len(fields) != 2 || state.enabled {
			return nativeState{}, errors.New("native DSCP trust setting is malformed or repeated")
		}
		state.enabled = true
	}
	return state, nil
}

func (s nativeState) validate(enabled bool) error {
	if enabled && s.symmetricFlowControl {
		return errors.New("DSCP trust is incompatible with global symmetrical flow control; disable that configuration separately before reconciling DSCP trust")
	}
	return nil
}
