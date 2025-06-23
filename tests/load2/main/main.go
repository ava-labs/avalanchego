// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
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
)

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags()
	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger("")
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()

	require := require.New(tc)
	ctx := context.Background()

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
	tc.DeferCleanup(func() {
		require.NoError(network.Stop(ctx), "failed to stop network")
	})

	wsURIs, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID, tc.DeferCleanup)
	require.NoError(err)

	registry := prometheus.NewRegistry()
	metrics, err := load2.NewMetrics(metricsNamespace, registry)
	require.NoError(err)

	wallets := make([]*load2.Wallet, len(keys))
	txTests := make([]load2.Test, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		chainID, err := client.ChainID(ctx)
		require.NoError(err)

		wallets[i] = load2.NewWallet(metrics, client, keys[i].ToECDSA(), 0, chainID)

		txTests[i] = load2.ZeroTransferTest{PollFrequency: pollFrequency}
	}

	generator, err := load2.NewGenerator(wallets, txTests)
	require.NoError(err)

	generator.Run(tc, ctx)
}
