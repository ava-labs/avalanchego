// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
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

	loadTimeout int
)

func init() {
	flagVars = e2e.RegisterFlags()

	flag.IntVar(
		&loadTimeout,
		"load-timeout",
		0,
		"the duration that the load test should run for (in seconds)",
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
	metrics, err := load2.NewMetrics(metricsNamespace, registry)
	require.NoError(err)

	wallets := make([]*load2.Wallet, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		chainID, err := client.ChainID(ctx)
		require.NoError(err)

		wallets[i] = load2.NewWallet(metrics, client, keys[i].ToECDSA(), 0, chainID)
	}

	generator, err := load2.NewGenerator(
		wallets,
		load2.ZeroTransferTest{PollFrequency: pollFrequency},
	)
	require.NoError(err)

	loadTimeout := time.Duration(loadTimeout) * time.Second
	generator.Run(tc, ctx, loadTimeout, testTimeout)
}
