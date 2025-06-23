// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load2"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const (
	blockchainID  = "C"
	nodesCount    = 5
	agentsPerNode = 5
	agentsCount   = nodesCount * agentsPerNode
	logPrefix     = "avalanchego-load-test"
	namespace     = "load"
	pollFrequency = time.Millisecond
)

var (
	flagVars *e2e.FlagVars

	errFailedToCreateContract = errors.New("failed to create contract")
)

func init() {
	flagVars = e2e.RegisterFlags()
	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger(logPrefix)
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()

	require := require.New(tc)
	ctx := context.Background()

	nodes := tmpnet.NewNodesOrPanic(nodesCount)

	keys, err := tmpnet.NewPrivateKeys(agentsCount)
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
	tracker, err := load2.NewTracker(namespace, registry)
	require.NoError(err)

	issuanceF := func(i common.IssuanceReceipt) {
		tracker.Issue(i.Duration)
	}
	confirmationF := func(c common.ConfirmationReceipt) {
		tracker.Accept(c.ConfirmationDuration, c.TotalDuration)
	}

	wallets := make([]load2.Wallet, len(keys))
	txTests := make([]load2.TxTest, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		chainID, err := client.ChainID(ctx)
		require.NoError(err)

		wallet := load2.NewWallet(client, keys[i].ToECDSA(), 0, chainID)

		wallets[i] = load2.NewWalletWithOptions(
			wallet,
			common.WithIssuanceHandler(issuanceF),
			common.WithConfirmationHandler(confirmationF),
			common.WithPollFrequency(pollFrequency),
		)
		txTests[i] = load2.TestZeroTransfer
	}

	generator, err := load2.NewGenerator(wallets, txTests)
	require.NoError(err)

	generator.Run(tc, ctx)
}
