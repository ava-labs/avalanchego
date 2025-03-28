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
)

type FlagVars struct {
	runtime             string
	avalancheGoExecPath string
	kubeConfigPath      string
	// TODO(marun) Include the context in the kube runtime config
	kubeConfigContext string
	kubeNamespace     string
	// TODO(marun) Rename
	imageName         string
	nodeRuntimeConfig *tmpnet.NodeRuntimeConfig
	pluginDir         string
	networkDir        string
	reuseNetwork      bool
	startCollectors   bool
	checkMonitoring   bool
	startNetwork      bool
	stopNetwork       bool
	restartNetwork    bool
	nodeCount         int
	activateFortuna   bool
}

func (v *FlagVars) NodeRuntimeConfig() *tmpnet.NodeRuntimeConfig {
	if v.nodeRuntimeConfig == nil {
		switch v.runtime {
		case "process":
			v.nodeRuntimeConfig = &tmpnet.NodeRuntimeConfig{
				AvalancheGoPath: v.avalancheGoExecPath,
			}
		case "kube":
			v.nodeRuntimeConfig = &tmpnet.NodeRuntimeConfig{
				KubeRuntimeConfig: &tmpnet.KubeRuntimeConfig{
					Kubeconfig: v.kubeConfigPath,
					Namespace:  v.kubeNamespace,
					ImageName:  v.imageName,
				},
			}
		default:
			// TODO(marun) Make this an error condition instead of a panic
			panic("unknown runtime: " + v.runtime)
		}
	}
	return v.nodeRuntimeConfig
}

func (v *FlagVars) PluginDir() string {
	// TODO(marun) Will need to support this properly for VMs like subnet-evm
	if v.runtime != "kube" {
		return v.pluginDir
	}
	return ""
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

func (v *FlagVars) StartCollectors() bool {
	return v.startCollectors
}

func (v *FlagVars) CheckMonitoring() bool {
	return v.checkMonitoring
}

func (v *FlagVars) NetworkShutdownDelay() time.Duration {
	if v.startCollectors {
		// Only return a non-zero value if we want to ensure the collectors have
		// a chance to collect the metrics at the end of the test.
		return tmpnet.NetworkShutdownDelay
	}
	return 0
}

func (v *FlagVars) StartNetwork() bool {
	return v.startNetwork
}

func (v *FlagVars) StopNetwork() bool {
	return v.stopNetwork
}

func (v *FlagVars) NodeCount() int {
	return v.nodeCount
}

func (v *FlagVars) ActivateFortuna() bool {
	return v.activateFortuna
}

func RegisterFlags() *FlagVars {
	vars := FlagVars{}
	flag.StringVar(
		&vars.runtime,
		"runtime",
		"process",
		"[optional] the runtime to use to deploy nodes for the network. Valid options are 'process' and 'kube'.",
	)
	flag.StringVar(
		&vars.avalancheGoExecPath,
		"avalanchego-path",
		os.Getenv(tmpnet.AvalancheGoPathEnvName),
		fmt.Sprintf(
			"[optional] avalanchego executable path if creating a new network. Also possible to configure via the %s env variable.",
			tmpnet.AvalancheGoPathEnvName,
		),
	)
	SetKubeFlags(flag.StringVar, &vars.kubeConfigPath, &vars.kubeConfigContext, &vars.kubeNamespace)
	flag.StringVar(
		&vars.imageName,
		"image-name",
		"avaplatform/avalanchego:latest",
		"The name of the docker image to use for creating nodes",
	)
	flag.StringVar(
		&vars.pluginDir,
		"plugin-dir",
		tmpnet.GetEnvWithDefault(tmpnet.AvalancheGoPluginDirEnvName, os.ExpandEnv("$HOME/.avalanchego/plugins")),
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
		"[optional] reuse an existing network previously started with --reuse-network. If a network is not already running, create a new one and leave it running for subsequent usage. Ignored if --stop-network is provided.",
	)
	flag.BoolVar(
		&vars.restartNetwork,
		"restart-network",
		false,
		"[optional] restart an existing network previously started with --reuse-network. Useful for ensuring a network is running with the current state of binaries on disk. Ignored if a network is not already running or --stop-network is provided.",
	)
	SetMonitoringFlags(
		&vars.startCollectors,
		&vars.checkMonitoring,
	)
	flag.BoolVar(
		&vars.startNetwork,
		"start-network",
		false,
		"[optional] start a new network and exit without executing any tests. The new network cannot be reused with --reuse-network. Ignored if either --reuse-network or --stop-network is provided.",
	)
	flag.BoolVar(
		&vars.stopNetwork,
		"stop-network",
		false,
		"[optional] stop an existing network started with --reuse-network and exit without executing any tests.",
	)
	flag.IntVar(
		&vars.nodeCount,
		"node-count",
		tmpnet.DefaultNodeCount,
		"number of nodes the network should initially consist of",
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
	SetStartCollectorsFlag(flag.BoolVar, startCollectors)
	flag.BoolVar(
		checkMonitoring,
		"check-monitoring",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_CHECK_MONITORING", "false")),
		"[optional] whether to check that logs and metrics have been collected from nodes of the temporary network.",
	)
}

type StringVarFunc func(p *string, name string, value string, usage string)

func SetKubeFlags(stringVar StringVarFunc, kubeConfigPath *string, kubeConfigContext *string, namespace *string) {
	stringVar(
		kubeConfigPath,
		"kubeconfig",
		tmpnet.GetEnvWithDefault("KUBECONFIG", os.ExpandEnv("$HOME/.kube/config")),
		"The path to a kubernetes configuration file for the target cluster",
	)
	stringVar(
		kubeConfigContext,
		"kubeconfig-context",
		"",
		"The path to a kubernetes configuration file for the target cluster",
	)
	stringVar(
		namespace,
		"namespace",
		tmpnet.DefaultTmpnetNamespace,
		"The namespace in the target cluster to create nodes in",
	)
}

// Enable reuse by tmpnetctl
type BoolVarFunc func(p *bool, name string, value bool, usage string)

func SetStartCollectorsFlag(boolVar BoolVarFunc, startCollectors *bool) {
	boolVar(
		startCollectors,
		"start-collectors",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_START_COLLECTORS", "false")),
		"[optional] whether to start collectors of logs and metrics from nodes of the temporary network.",
	)
}
