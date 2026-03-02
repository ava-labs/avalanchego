// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

var validRuntimes = []string{
	processRuntime,
	kubeRuntime,
}

type RuntimeConfigVars struct {
	runtime            string
	processRuntimeVars processRuntimeVars
	kubeRuntimeVars    kubeRuntimeVars
}

// NewRuntimeConfigFlagVars registers runtime config flag variables for stdlib flag
func NewRuntimeConfigFlagVars() *RuntimeConfigVars {
	v := &RuntimeConfigVars{}
	v.processRuntimeVars.registerWithFlag()
	v.kubeRuntimeVars.registerWithFlag()
	v.register(flag.StringVar)
	return v
}

// NewRuntimeConfigFlagSetVars registers runtime config flag variables for pflag
func NewRuntimeConfigFlagSetVars(flagSet *pflag.FlagSet) *RuntimeConfigVars {
	v := &RuntimeConfigVars{}
	v.processRuntimeVars.registerWithFlagSet(flagSet)
	v.kubeRuntimeVars.registerWithFlagSet(flagSet)
	v.register(flagSet.StringVar)
	return v
}

func (v *RuntimeConfigVars) register(stringVar varFunc[string]) {
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
			return nil, stacktrace.Errorf("failed to configure %s runtime: %w", processRuntime, err)
		}
		return &tmpnet.NodeRuntimeConfig{
			Process: processRuntimeConfig,
		}, nil
	case kubeRuntime:
		kubeRuntimeConfig, err := v.kubeRuntimeVars.getKubeRuntimeConfig()
		if err != nil {
			return nil, stacktrace.Wrap(err)
		}
		return &tmpnet.NodeRuntimeConfig{
			Kube: kubeRuntimeConfig,
		}, nil
	default:
		return nil, stacktrace.Errorf("--runtime expected one of %v, got: %s", validRuntimes, v.runtime)
	}
}
