// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

var validRuntimes = []string{
	processRuntime,
}

type RuntimeConfigVars struct {
	runtime            string
	processRuntimeVars processRuntimeVars
}

// NewRuntimeConfigFlagVars registers runtime config flag variables for stdlib flag
func NewRuntimeConfigFlagVars() *RuntimeConfigVars {
	v := &RuntimeConfigVars{}
	v.processRuntimeVars.registerWithFlag()
	v.register(flag.StringVar)
	return v
}

// NewRuntimeConfigFlagSetVars registers runtime config flag variables for pflag
func NewRuntimeConfigFlagSetVars(flagSet *pflag.FlagSet) *RuntimeConfigVars {
	v := &RuntimeConfigVars{}
	v.processRuntimeVars.registerWithFlagSet(flagSet)
	v.register(flagSet.StringVar)
	return v
}

func (v *RuntimeConfigVars) register(stringVar stringVarFunc) {
	stringVar(
		&v.runtime,
		"runtime",
		processRuntime,
		fmt.Sprintf(
			"The runtime to use to deploy nodes for the network. Valid options are %v.",
			validRuntimes,
		),
	)
}

// GetNodeRuntimeConfig returns the validated node runtime config
func (v *RuntimeConfigVars) GetNodeRuntimeConfig() (*tmpnet.NodeRuntimeConfig, error) {
	switch v.runtime {
	case processRuntime:
		processRuntimeConfig, err := v.processRuntimeVars.getProcessRuntimeConfig()
		if err != nil {
			return nil, err
		}
		return &tmpnet.NodeRuntimeConfig{
			Process: processRuntimeConfig,
		}, nil
	default:
		return nil, fmt.Errorf("--runtime expected one of %v, got: %s", validRuntimes, v.runtime)
	}
}
