package dscptrust

import (
	"errors"

	"github.com/zariel/fastiron-tofu/internal/config"
)

type nativeState struct {
	enabled              bool
	symmetricFlowControl bool
	unowned              []string
}

func parse(document *config.Document, name string) (nativeState, error) {
	observed, err := document.InterfacePolicy(name, config.DSCPTrust)
	if err != nil {
		return nativeState{}, err
	}
	return nativeState{enabled: observed.Enabled, symmetricFlowControl: document.SymmetricFlowControl(), unowned: observed.Remaining}, nil
}

func (s nativeState) validate(enabled bool) error {
	if enabled && s.symmetricFlowControl {
		return errors.New("DSCP trust is incompatible with global symmetrical flow control; disable that configuration separately before reconciling DSCP trust")
	}
	return nil
}
