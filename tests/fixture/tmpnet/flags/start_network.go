// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

type StartNetworkVars struct {
	// Accessible directly
	RootNetworkDir string
	NetworkOwner   string

	// Accessible via a validating method
	nodeCount   int
	runtimeVars *RuntimeConfigVars

	defaultNetworkOwner string
}

func NewStartNetworkFlagVars(defaultNetworkOwner string) *StartNetworkVars {
	v := &StartNetworkVars{defaultNetworkOwner: defaultNetworkOwner}
	v.runtimeVars = NewRuntimeConfigFlagVars()
	v.register(flag.StringVar, flag.IntVar)
	return v
}

func NewStartNetworkFlagSetVars(flagSet *pflag.FlagSet, defaultNetworkOwner string) *StartNetworkVars {
	v := &StartNetworkVars{defaultNetworkOwner: defaultNetworkOwner}
	v.runtimeVars = NewRuntimeConfigFlagSetVars(flagSet)
	v.register(flagSet.StringVar, flagSet.IntVar)
	return v
}

func (v *StartNetworkVars) register(stringVar stringVarFunc, intVar intVarFunc) {
	stringVar(
		&v.RootNetworkDir,
		"root-network-dir",
		// An empty string prompts the use of the default path.
		tmpnet.GetEnvWithDefault(tmpnet.RootNetworkDirEnvName, ""),
		fmt.Sprintf("The directory in which to create the network directory. Also possible to configure via the %s env variable.", tmpnet.RootNetworkDirEnvName),
	)
	stringVar(
		&v.NetworkOwner,
		"network-owner",
		v.defaultNetworkOwner,
		"The string identifying the intended owner of the network")
	intVar(
		&v.nodeCount,
		"node-count",
		tmpnet.DefaultNodeCount,
		"Number of nodes the network should initially consist of",
	)
}

func (v *StartNetworkVars) GetNodeCount() (int, error) {
	if v.nodeCount < 1 {
		return 0, fmt.Errorf("--node-count must be greater than 0 but got %d", v.nodeCount)
	}
	return v.nodeCount, nil
}

func (v *StartNetworkVars) GetNodeRuntimeConfig() (*tmpnet.NodeRuntimeConfig, error) {
	return v.runtimeVars.GetNodeRuntimeConfig()
}
