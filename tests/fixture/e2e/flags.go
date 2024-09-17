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

// Ensure that this value takes into account the scrape_interval
// defined in scripts/run_prometheus.sh.
const networkShutdownDelay = 12 * time.Second

type FlagVars struct {
	avalancheGoExecPath  string
	pluginDir            string
	networkDir           string
	reuseNetwork         bool
	delayNetworkShutdown bool
	stopNetwork          bool
	restartNetwork       bool
	nodeCount            int
	activateEtna         bool
}

func (v *FlagVars) AvalancheGoExecPath() string {
	return v.avalancheGoExecPath
}

func (v *FlagVars) PluginDir() string {
	return v.pluginDir
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

func (v *FlagVars) NetworkShutdownDelay() time.Duration {
	if v.delayNetworkShutdown {
		// Only return a non-zero value if the delay is enabled.
		return networkShutdownDelay
	}
	return 0
}

func (v *FlagVars) StopNetwork() bool {
	return v.stopNetwork
}

func (v *FlagVars) NodeCount() int {
	return v.nodeCount
}

func (v *FlagVars) ActivateEtna() bool {
	return v.activateEtna
}

func getEnvWithDefault(envVar, defaultVal string) string {
	val := os.Getenv(envVar)
	if len(val) == 0 {
		return defaultVal
	}
	return val
}

func RegisterFlags() *FlagVars {
	vars := FlagVars{}
	flag.StringVar(
		&vars.avalancheGoExecPath,
		"avalanchego-path",
		os.Getenv(tmpnet.AvalancheGoPathEnvName),
		fmt.Sprintf(
			"[optional] avalanchego executable path if creating a new network. Also possible to configure via the %s env variable.",
			tmpnet.AvalancheGoPathEnvName,
		),
	)
	flag.StringVar(
		&vars.pluginDir,
		"plugin-dir",
		getEnvWithDefault(tmpnet.AvalancheGoPluginDirEnvName, os.ExpandEnv("$HOME/.avalanchego/plugins")),
		fmt.Sprintf(
			"[optional] the dir containing VM plugins. Also possible to configure via the %s env variable.",
			tmpnet.AvalancheGoPluginDirEnvName,
		),
	)
	flag.StringVar(
		&vars.networkDir,
		"network-dir",
		"",
		fmt.Sprintf("[optional] the dir containing the configuration of an existing network to target for testing. Will only be used if --reuse-network is specified. Also possible to configure via the %s env variable.", tmpnet.NetworkDirEnvName),
	)
	flag.BoolVar(
		&vars.reuseNetwork,
		"reuse-network",
		false,
		"[optional] reuse an existing network. If an existing network is not already running, create a new one and leave it running for subsequent usage.",
	)
	flag.BoolVar(
		&vars.restartNetwork,
		"restart-network",
		false,
		"[optional] restarts an existing network. Useful for ensuring a network is running with the current state of binaries on disk. Ignored if a network is not already running or --stop-network is provided.",
	)
	flag.BoolVar(
		&vars.delayNetworkShutdown,
		"delay-network-shutdown",
		false,
		"[optional] whether to delay network shutdown to allow a final metrics scrape.",
	)
	flag.BoolVar(
		&vars.stopNetwork,
		"stop-network",
		false,
		"[optional] stop an existing network and exit without executing any tests.",
	)
	flag.IntVar(
		&vars.nodeCount,
		"node-count",
		tmpnet.DefaultNodeCount,
		"number of nodes the network should initially consist of",
	)
	flag.BoolVar(
		&vars.activateEtna,
		"activate-etna",
		false,
		"[optional] activate the etna upgrade",
	)

	return &vars
}
