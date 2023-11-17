// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"flag"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"
)

type FlagVars struct {
	avalancheGoExecPath  string
	persistentNetworkDir string
	usePersistentNetwork bool
}

func (v *FlagVars) PersistentNetworkDir() string {
	if v.usePersistentNetwork && len(v.persistentNetworkDir) == 0 {
		return os.Getenv(local.NetworkDirEnvName)
	}
	return v.persistentNetworkDir
}

func (v *FlagVars) AvalancheGoExecPath() string {
	return v.avalancheGoExecPath
}

func (v *FlagVars) UsePersistentNetwork() bool {
	return v.usePersistentNetwork
}

func RegisterFlags() *FlagVars {
	vars := FlagVars{}
	flag.StringVar(
		&vars.avalancheGoExecPath,
		"avalanchego-path",
		os.Getenv(local.AvalancheGoPathEnvName),
		fmt.Sprintf("avalanchego executable path (required if not using a persistent network). Also possible to configure via the %s env variable.", local.AvalancheGoPathEnvName),
	)
	flag.StringVar(
		&vars.persistentNetworkDir,
		"network-dir",
		"",
		fmt.Sprintf("[optional] the dir containing the configuration of a persistent network to target for testing. Useful for speeding up test development. Also possible to configure via the %s env variable.", local.NetworkDirEnvName),
	)
	flag.BoolVar(
		&vars.usePersistentNetwork,
		"use-persistent-network",
		false,
		"[optional] whether to target the persistent network identified by --network-dir.",
	)

	return &vars
}
