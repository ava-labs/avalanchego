// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"flag"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load/c/contracts"
	"github.com/ava-labs/avalanchego/tests/load2"
)

const (
	blockchainID = "C"
	// invariant: nodesCount >= 5
	nodesCount    = 5
	agentsPerNode = 5
	agentsCount   = nodesCount * agentsPerNode
	logPrefix     = "avalanchego-load-test"
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
		Owner:         "avalanchego-load-test",
		Nodes:         nodes,
		PreFundedKeys: keys,
	}

	e2e.NewTestEnvironment(tc, flagVars, network)
	tc.DeferCleanup(func() {
		require.NoError(network.Stop(ctx), "failed to stop network")
	})

	wsURIs, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID, tc.DeferCleanup)
	require.NoError(err)

	wallets := make([]*load2.Wallet, len(keys))
	txTests := make([]load2.TxTest, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		chainID, err := client.ChainID(ctx)
		require.NoError(err)

		wallet := load2.NewWallet(client, keys[i].ToECDSA(), 0, chainID)
		contract, err := createContract(ctx, client, wallet)
		require.NoError(err)

		txTest := testWithContract(load2.TestRandomTx, contract)

		wallets[i] = wallet
		txTests[i] = txTest
	}

	generator, err := load2.NewGenerator(log, wallets, txTests)
	require.NoError(err)

	require.NoError(generator.Run(tc, ctx))
}

func createContract(
	ctx context.Context,
	client *ethclient.Client,
	wallet *load2.Wallet,
) (*contracts.EVMLoadSimulator, error) {
	maxFeeCap := big.NewInt(300000000000)
	txOpts, err := load2.NewTxOpts(wallet.PrivKey(), wallet.ChainID(), maxFeeCap, wallet.Nonce())
	if err != nil {
		return nil, err
	}

	_, tx, _, err := contracts.DeployEVMLoadSimulator(txOpts, client)
	if err != nil {
		return nil, err
	}

	var contractAddress common.Address
	if err := wallet.SendTx(
		ctx,
		tx,
		500*time.Millisecond,
		func(time.Duration) {},
		func(receipt *types.Receipt, _ time.Duration) {
			contractAddress = receipt.ContractAddress
		},
	); err != nil {
		return nil, err
	}

	if contractAddress == (common.Address{}) {
		return nil, errFailedToCreateContract
	}

	return contracts.NewEVMLoadSimulator(contractAddress, client)
}

func testWithContract(
	f func(tests.TestContext, context.Context, *load2.Wallet, *contracts.EVMLoadSimulator),
	contract *contracts.EVMLoadSimulator,
) load2.TxTest {
	return func(tc tests.TestContext, ctx context.Context, w *load2.Wallet) {
		f(tc, ctx, w, contract)
	}
}
