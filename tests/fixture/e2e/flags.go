// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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

	collectorVars *flags.CollectorVars

	checkMetricsCollected bool
	checkLogsCollected    bool

	networkDir     string
	reuseNetwork   bool
	stopNetwork    bool
	restartNetwork bool

	activateHelicon bool
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

func (v *FlagVars) StartMetricsCollector() bool {
	return v.collectorVars.StartMetricsCollector
}

func (v *FlagVars) StartLogsCollector() bool {
	return v.collectorVars.StartLogsCollector
}

func (v *FlagVars) CheckMetricsCollected() bool {
	return v.checkMetricsCollected
}

func (v *FlagVars) CheckLogsCollected() bool {
	return v.checkLogsCollected
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
	if v.StartMetricsCollector() {
		// Only return a non-zero value if we want to ensure the collectors have
		// a chance to collect the metrics at the end of the test.
		return tmpnet.NetworkShutdownDelay
	}
	return 0
}

func (v *FlagVars) ActivateHelicon() bool {
	return v.activateHelicon
}

type DefaultOption func(*DefaultOptions)

type DefaultOptions struct {
	owner     string
	nodeCount int
}

func newDefaultOptions(ops []DefaultOption) *DefaultOptions {
	o := &DefaultOptions{}
	for _, op := range ops {
		op(o)
	}
	return o
}

func (d *DefaultOptions) Owner() string {
	return d.owner
}

func (d *DefaultOptions) NodeCount() int {
	if d.nodeCount <= 0 {
		return tmpnet.DefaultNodeCount
	}

	return d.nodeCount
}

func WithDefaultOwner(owner string) DefaultOption {
	return func(d *DefaultOptions) {
		d.owner = owner
	}
}

func WithDefaultNodeCount(nodeCount int) DefaultOption {
	return func(d *DefaultOptions) {
		d.nodeCount = nodeCount
	}
}

func RegisterFlags(ops ...DefaultOption) *FlagVars {
	vars := FlagVars{}

	flag.BoolVar(
		&vars.startNetwork,
		"start-network",
		false,
		"[optional] start a new network and exit without executing any tests. The new network cannot be reused with --reuse-network.",
	)

	options := newDefaultOptions(ops)
	vars.startNetworkVars = flags.NewStartNetworkFlagVars(
		options.Owner(),
		options.NodeCount(),
	)

	vars.collectorVars = flags.NewCollectorFlagVars()

	SetCheckCollectionFlags(
		&vars.checkMetricsCollected,
		&vars.checkLogsCollected,
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
		&vars.activateHelicon,
		"activate-helicon",
		false,
		"[optional] activate the helicon upgrade",
	)

	return &vars
}

func SetCheckCollectionFlags(checkMetricsCollected *bool, checkLogsCollected *bool) {
	flag.BoolVar(
		checkMetricsCollected,
		"check-metrics-collected",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_CHECK_METRICS_COLLECTED", "false")),
		"[optional] whether to check that metrics have been collected from nodes of the temporary network.",
	)
	flag.BoolVar(
		checkLogsCollected,
		"check-logs-collected",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_CHECK_LOGS_COLLECTED", "false")),
		"[optional] whether to check that logs have been collected from nodes of the temporary network.",
	)
}
