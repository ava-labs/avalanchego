// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"math/rand/v2"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/load/c/contracts"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

var maxFeeCap = big.NewInt(300000000000)

func TestRandomTx(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txTypes := []txType{
		{
			txFunc: func(tc tests.TestContext, ctx context.Context, w Wallet, _ *contracts.EVMLoadSimulator) {
				testZeroTransfer(tc, ctx, w)
			},
			weight: 1000,
		},
		{
			txFunc: testRandomWrite,
			weight: 100,
		},
		{
			txFunc: testStateModification,
			weight: 100,
		},
		{
			txFunc: testRandomRead,
			weight: 200,
		},
		{
			txFunc: testHashing,
			weight: 50,
		},
		{
			txFunc: testMemory,
			weight: 100,
		},
		{
			txFunc: testCallDepth,
			weight: 50,
		},
		{
			txFunc: testContractCreation,
			weight: 1,
		},
		{
			txFunc: testPureCompute,
			weight: 100,
		},
		{
			txFunc: testLargeEvent,
			weight: 100,
		},
		{
			txFunc: testExternallCall,
			weight: 50,
		},
	}

	// define weights and sampler
	weights := make([]uint64, len(txTypes))
	totalWeight := uint64(0)
	for i, builder := range txTypes {
		weights[i] = builder.weight
		totalWeight += builder.weight
	}

	weightedSampler := sampler.NewWeighted()
	require.NoError(weightedSampler.Initialize(weights))

	index, ok := weightedSampler.Sample(rand.Uint64N(totalWeight)) //#nosec G404
	require.True(ok)

	txF := txTypes[index].txFunc
	txF(tc, ctx, wallet, contract)
}

func NewTxOpts(
	key *ecdsa.PrivateKey,
	chainID *big.Int,
	maxFeeCap *big.Int,
	nonce uint64,
) (*bind.TransactOpts, error) {
	txOpts, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, err
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	txOpts.GasFeeCap = maxFeeCap
	txOpts.GasTipCap = common.Big1
	txOpts.NoSend = true
	return txOpts, nil
}

type txType struct {
	txFunc func(tests.TestContext, context.Context, Wallet, *contracts.EVMLoadSimulator)
	weight uint64
}

func testZeroTransfer(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
) {
	require := require.New(tc)

	maxValue := int64(100 * 1_000_000_000 / params.TxGas)
	maxFeeCap := big.NewInt(maxValue)
	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)
	senderAddress := crypto.PubkeyToAddress(wallet.PrivKey().PublicKey)
	tx, err := types.SignNewTx(wallet.PrivKey(), wallet.Signer(), &types.DynamicFeeTx{
		ChainID:   wallet.ChainID(),
		Nonce:     wallet.Nonce(),
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       params.TxGas,
		To:        &senderAddress,
		Data:      nil,
		Value:     common.Big0,
	})
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testRandomWrite(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const maxWriteSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxWriteSizeBytes)) //#nosec G404
	tx, err := contract.SimulateRandomWrite(txOpts, count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testStateModification(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const maxStateSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxStateSizeBytes)) //#nosec G404
	tx, err := contract.SimulateModification(txOpts, count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testRandomRead(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const maxReadSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxReadSizeBytes)) //#nosec G404
	tx, err := contract.SimulateReads(txOpts, count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testHashing(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const maxRounds = 3
	count := big.NewInt(rand.Int64N(maxRounds)) //#nosec G404
	tx, err := contract.SimulateHashing(txOpts, count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testMemory(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const maxRounds = 3
	count := big.NewInt(rand.Int64N(maxRounds)) //#nosec G404
	tx, err := contract.SimulateMemory(txOpts, count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testCallDepth(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const maxDepth = 5
	count := big.NewInt(rand.Int64N(maxDepth)) //#nosec G404
	tx, err := contract.SimulateCallDepth(txOpts, count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testContractCreation(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	tx, err := contract.SimulateContractCreation(txOpts)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testPureCompute(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const iterations = 100
	tx, err := contract.SimulatePureCompute(txOpts, big.NewInt(iterations))
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testLargeEvent(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	const maxEventSize = 100
	tx, err := contract.SimulateLargeEvent(txOpts, big.NewInt(maxEventSize))
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}

func testExternallCall(
	tc tests.TestContext,
	ctx context.Context,
	wallet Wallet,
	contract *contracts.EVMLoadSimulator,
) {
	require := require.New(tc)

	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	require.NoError(err)

	tx, err := contract.SimulateExternalCall(txOpts)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
}
