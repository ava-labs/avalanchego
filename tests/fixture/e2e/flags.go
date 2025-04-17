// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cast"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
)

type NetworkCmd int

const (
	EmptyNetworkCmd NetworkCmd = iota
	StartNetworkCmd
	StopNetworkCmd
	RestartNetworkCmd
	ReuseNetworkCmd
)

type FlagVars struct {
	startNetwork     bool
	startNetworkVars *flags.StartNetworkVars

	startCollectors bool
	checkMonitoring bool

	networkDir     string
	reuseNetwork   bool
	stopNetwork    bool
	restartNetwork bool

	activateFortuna bool
}

func (v *FlagVars) NetworkCmd() (NetworkCmd, error) {
	cmd := EmptyNetworkCmd
	count := 0
	if v.startNetwork {
		cmd = StartNetworkCmd
		count++
	}
	if v.stopNetwork {
		cmd = StopNetworkCmd
		count++
	}
	if v.restartNetwork {
		cmd = RestartNetworkCmd
		count++
	}
	if v.reuseNetwork {
		cmd = ReuseNetworkCmd
		count++
	}
	if count > 1 {
		return EmptyNetworkCmd, errors.New("only one of --start-network, --stop-network, --restart-network, or --reuse-network can be specified")
	}

	return cmd, nil
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
		"[optional] start a new network and exit without executing any tests. The new network cannot be reused with --reuse-network.",
	)

	vars.startNetworkVars = flags.NewStartNetworkFlagVars(defaultOwner)

	SetMonitoringFlags(
		&vars.startCollectors,
		&vars.checkMonitoring,
	)

	flag.StringVar(
		&vars.networkDir,
		"network-dir",
		tmpnet.GetEnvWithDefault(tmpnet.NetworkDirEnvName, ""),
		fmt.Sprintf("[optional] the dir containing the configuration of an existing network. Will only be used if --reuse-network, --restart-network or --stop-network are specified. Also possible to configure via the %s env variable.", tmpnet.NetworkDirEnvName),
	)

	flag.BoolVar(
		&vars.reuseNetwork,
		"reuse-network",
		false,
		"[optional] run tests against an existing network previously started with --reuse-network. If a network is not already running, create a new one and leave it running for subsequent usage.",
	)

	flag.BoolVar(
		&vars.restartNetwork,
		"restart-network",
		false,
		"[optional] like --reuse-network except an already running network is restarted before running tests to ensure the network represents the current state of binaries on disk.",
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
