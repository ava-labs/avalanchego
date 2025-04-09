// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
)

type FlagVars struct {
	startNetwork     bool
	startNetworkVars *flags.StartNetworkVars

	startCollectors bool
	checkMonitoring bool

	networkDir     string
	reuseNetwork   bool
	restartNetwork bool
	stopNetwork    bool

	activateFortuna bool
}

func (v *FlagVars) StartNetwork() bool {
	return v.startNetwork
}

func (v *FlagVars) RootNetworkDir() string {
	return v.startNetworkVars.RootNetworkDir
}

func (v *FlagVars) NetworkOwner() string {
	return v.startNetworkVars.NetworkOwner
}

func (v *FlagVars) NodeCount() (int, error) {
	return v.startNetworkVars.GetNodeCount()
}

func (v *FlagVars) NodeRuntimeConfig() (*tmpnet.NodeRuntimeConfig, error) {
	return v.startNetworkVars.GetNodeRuntimeConfig()
}

func (v *FlagVars) StartCollectors() bool {
	return v.startCollectors
}

func (v *FlagVars) CheckMonitoring() bool {
	return v.checkMonitoring
}

func (v *FlagVars) NetworkDir() string {
	if !v.reuseNetwork {
		return ""
	}
	if len(v.networkDir) > 0 {
		return v.networkDir
	}
	return os.Getenv(tmpnet.NetworkDirEnvName)
}

func (v *FlagVars) ReuseNetwork() bool {
	return v.reuseNetwork
}

func (v *FlagVars) RestartNetwork() bool {
	return v.restartNetwork
}

func (v *FlagVars) StopNetwork() bool {
	return v.stopNetwork
}

func (v *FlagVars) NetworkShutdownDelay() time.Duration {
	if v.startCollectors {
		// Only return a non-zero value if we want to ensure the collectors have
		// a chance to collect the metrics at the end of the test.
		return tmpnet.NetworkShutdownDelay
	}
	return 0
}

func (v *FlagVars) ActivateFortuna() bool {
	return v.activateFortuna
}

func RegisterFlags() *FlagVars {
	return RegisterFlagsWithDefaultOwner("")
}

func RegisterFlagsWithDefaultOwner(defaultOwner string) *FlagVars {
	vars := FlagVars{}

	flag.BoolVar(
		&vars.startNetwork,
		"start-network",
		false,
		"[optional] start a new network and exit without executing any tests. The new network cannot be reused with --reuse-network. Ignored if either --reuse-network or --stop-network is provided.",
	)

	vars.startNetworkVars = flags.NewStartNetworkFlagVars(defaultOwner)

	SetMonitoringFlags(
		&vars.startCollectors,
		&vars.checkMonitoring,
	)

	flag.StringVar(
		&vars.networkDir,
		"network-dir",
		"",
		fmt.Sprintf("[optional] the dir containing the configuration of an existing network. Will only be used if --reuse-network, --restart-network or --stop-network are specified. Also possible to configure via the %s env variable.", tmpnet.NetworkDirEnvName),
	)

	flag.BoolVar(
		&vars.reuseNetwork,
		"reuse-network",
		false,
		"[optional] reuse an existing network previously started with --reuse-network. If a network is not already running, create a new one and leave it running for subsequent usage. Ignored if --stop-network is provided.",
	)

	flag.BoolVar(
		&vars.restartNetwork,
		"restart-network",
		false,
		"[optional] restart an existing network previously started with --reuse-network. Useful for ensuring a network is running with the current state of binaries on disk. Ignored if a network is not already running or --stop-network is provided.",
	)

	flag.BoolVar(
		&vars.stopNetwork,
		"stop-network",
		false,
		"[optional] stop an existing network started with --reuse-network and exit without executing any tests.",
	)

	flag.BoolVar(
		&vars.activateFortuna,
		"activate-fortuna",
		false,
		"[optional] activate the fortuna upgrade",
	)

	return &vars
}

// Enable reuse by the upgrade job
func SetMonitoringFlags(startCollectors *bool, checkMonitoring *bool) {
	flag.BoolVar(
		startCollectors,
		"start-collectors",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_START_COLLECTORS", "false")),
		"[optional] whether to start collectors of logs and metrics from nodes of the temporary network.",
	)
	flag.BoolVar(
		checkMonitoring,
		"check-monitoring",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_CHECK_MONITORING", "false")),
		"[optional] whether to check that logs and metrics have been collected from nodes of the temporary network.",
	)
}
