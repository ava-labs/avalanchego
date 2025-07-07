// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"os"
	"time"

	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load2"
)

const (
	blockchainID     = "C"
	metricsNamespace = "load"
	pollFrequency    = time.Millisecond
	testTimeout      = time.Minute
)

var (
	flagVars *e2e.FlagVars

	loadTimeout time.Duration
)

func init() {
	flagVars = e2e.RegisterFlags()

	flag.DurationVar(
		&loadTimeout,
		"load-timeout",
		0,
		"the duration that the load test should run for",
	)

	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger("")
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()

	require := require.New(tc)

	numNodes, err := flagVars.NodeCount()
	require.NoError(err, "failed to get node count")

	nodes := tmpnet.NewNodesOrPanic(numNodes)

	keys, err := tmpnet.NewPrivateKeys(numNodes)
	require.NoError(err)
	network := &tmpnet.Network{
		Nodes:         nodes,
		PreFundedKeys: keys,
	}

	e2e.NewTestEnvironment(tc, flagVars, network)

	ctx := tests.DefaultNotifyContext(0, tc.DeferCleanup)
	wsURIs, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID, tc.DeferCleanup)
	require.NoError(err)

	registry := prometheus.NewRegistry()
	metricsServer := tests.NewPrometheusServer("127.0.0.1:0", "/ext/metrics", registry)
	metricsErrChan, err := metricsServer.Start()
	require.NoError(err)

	monitoringConfigFilePath, err := tmpnet.WritePrometheusServiceDiscoveryConfigFile("load-test", []tmpnet.SDConfig{
		{
			Targets: []string{metricsServer.Address()},
			Labels:  network.GetMonitoringLabels(),
		},
	}, false)
	require.NoError(err, "failed to generate monitoring config file")

	tc.DeferCleanup(func() {
		require.NoError(metricsServer.Stop())
		require.NoError(<-metricsErrChan)
	})

	tc.DeferCleanup(func() {
		require.NoError(
			os.Remove(monitoringConfigFilePath),
			"failed †o remove monitoring config file",
		)
	})

	workers := make([]load2.Worker, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		workers[i] = load2.Worker{
			PrivKey: keys[i].ToECDSA(),
			Client:  client,
		}
	}

	chainID, err := workers[0].Client.ChainID(ctx)
	require.NoError(err)

	generator, err := load2.NewLoadGenerator(
		workers,
		chainID,
		metricsNamespace,
		registry,
		load2.ZeroTransferTest{PollFrequency: pollFrequency},
	)
	require.NoError(err)

	generator.Run(tc, ctx, loadTimeout, testTimeout)
}
