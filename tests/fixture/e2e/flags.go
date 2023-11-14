// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"flag"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/tests/fixture/ephnet/local"
)

type FlagVars struct {
	avalancheGoExecPath string
	networkDir          string
	useExistingNetwork  bool
}

func (v *FlagVars) NetworkDir() string {
	if !v.useExistingNetwork {
		return ""
	}
	if len(v.networkDir) > 0 {
		return v.networkDir
	}
	return os.Getenv(local.NetworkDirEnvName)
}

func (v *FlagVars) AvalancheGoExecPath() string {
	return v.avalancheGoExecPath
}

func (v *FlagVars) UseExistingNetwork() bool {
	return v.useExistingNetwork
}

func RegisterFlags() *FlagVars {
	vars := FlagVars{}
	flag.StringVar(
		&vars.avalancheGoExecPath,
		"avalanchego-path",
		os.Getenv(local.AvalancheGoPathEnvName),
		fmt.Sprintf("avalanchego executable path (required if not using an existing network). Also possible to configure via the %s env variable.", local.AvalancheGoPathEnvName),
	)
	flag.StringVar(
		&vars.networkDir,
		"network-dir",
		"",
		fmt.Sprintf("[optional] the dir containing the configuration of an existing network to target for testing. Will only be used if --use-existing-network is specified. Also possible to configure via the %s env variable.", local.NetworkDirEnvName),
	)
	flag.BoolVar(
		&vars.useExistingNetwork,
		"use-existing-network",
		false,
		"[optional] whether to target the existing network identified by --network-dir.",
	)

	return &vars
}
