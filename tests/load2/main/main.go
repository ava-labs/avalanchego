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
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
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

	endpoints, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID, tc.DeferCleanup)
	require.NoError(err)

	require.NoError(run(ctx, log, keys, endpoints))
}

func run(
	ctx context.Context,
	log logging.Logger,
	keys []*secp256k1.PrivateKey,
	wsURIs []string,
) error {
	wallets := make([]*load2.Wallet, len(keys))
	txBuilders := make([]load2.TxBuilder, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		if err != nil {
			return err
		}

		chainID, err := client.ChainID(ctx)
		if err != nil {
			return err
		}
		wallet := load2.NewWallet(client, keys[i].ToECDSA(), 0, chainID)
		contract, err := createContract(ctx, client, wallet)
		if err != nil {
			return err
		}
		txBuilder := load2.WithContractInstance(load2.BuildContractCreationTx, contract)

		wallets[i] = wallet
		txBuilders[i] = txBuilder
	}

	generator, err := load2.NewGenerator(log, wallets, txBuilders)
	if err != nil {
		return err
	}

	return generator.Run(ctx)
}

func createContract(
	ctx context.Context,
	client *ethclient.Client,
	wallet *load2.Wallet,
) (*contracts.EVMLoadSimulator, error) {
	backend := wallet.Backend()
	maxFeeCap := big.NewInt(300000000000)
	txOpts, err := load2.NewTxOpts(backend.PrivKey(), backend.ChainID(), maxFeeCap, backend.Nonce())
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
