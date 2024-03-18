// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

type FlagVars struct {
	avalancheGoExecPath  string
	pluginDir            string
	networkDir           string
	useExistingNetwork   bool
	networkShutdownDelay time.Duration
}

func (v *FlagVars) AvalancheGoExecPath() string {
	return v.avalancheGoExecPath
}

func (v *FlagVars) PluginDir() string {
	return v.pluginDir
}

func (v *FlagVars) NetworkDir() string {
	if !v.useExistingNetwork {
		return ""
	}
	if len(v.networkDir) > 0 {
		return v.networkDir
	}
	return os.Getenv(tmpnet.NetworkDirEnvName)
}

func (v *FlagVars) UseExistingNetwork() bool {
	return v.useExistingNetwork
}

func (v *FlagVars) NetworkShutdownDelay() time.Duration {
	return v.networkShutdownDelay
}

func RegisterFlags() *FlagVars {
	vars := FlagVars{}
	flag.StringVar(
		&vars.avalancheGoExecPath,
		"avalanchego-path",
		os.Getenv(tmpnet.AvalancheGoPathEnvName),
		fmt.Sprintf("avalanchego executable path (required if not using an existing network). Also possible to configure via the %s env variable.", tmpnet.AvalancheGoPathEnvName),
	)
	flag.StringVar(
		&vars.pluginDir,
		"plugin-dir",
		os.ExpandEnv("$HOME/.avalanchego/plugins"),
		"[optional] the dir containing VM plugins.",
	)
	flag.StringVar(
		&vars.networkDir,
		"network-dir",
		"",
		fmt.Sprintf("[optional] the dir containing the configuration of an existing network to target for testing. Will only be used if --use-existing-network is specified. Also possible to configure via the %s env variable.", tmpnet.NetworkDirEnvName),
	)
	flag.BoolVar(
		&vars.useExistingNetwork,
		"use-existing-network",
		false,
		"[optional] whether to target the existing network identified by --network-dir.",
	)
	flag.DurationVar(
		&vars.networkShutdownDelay,
		"network-shutdown-delay",
		12*time.Second, // Make sure this value takes into account the scrape_interval defined in scripts/run_prometheus.sh
		"[optional] the duration to wait before shutting down the test network at the end of the test run. A value greater than the scrape interval is suggested. 0 avoids waiting for shutdown.",
	)

	return &vars
}
