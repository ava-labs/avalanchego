// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"math/rand/v2"
	"time"

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

var (
	maxFeeCap = big.NewInt(300000000000)

	_ Test = (*RandomTest)(nil)
	_ Test = (*ZeroTransferTest)(nil)
	_ Test = (*ReadTest)(nil)
	_ Test = (*WriteTest)(nil)
	_ Test = (*StateModificationTest)(nil)
	_ Test = (*HashingTest)(nil)
	_ Test = (*MemoryTest)(nil)
	_ Test = (*CallDepthTest)(nil)
	_ Test = (*ContractCreationTest)(nil)
	_ Test = (*PureComputeTest)(nil)
	_ Test = (*LargeEventTest)(nil)
	_ Test = (*ExternalCallTest)(nil)
)

type RandomTest struct {
	tests       []Test
	weighted    sampler.Weighted
	totalWeight uint64
}

func NewRandomTest(weightedTests []WeightedTest) (RandomTest, error) {
	weighted := sampler.NewWeighted()

	// Initialize weighted set
	tests := make([]Test, len(weightedTests))
	weights := make([]uint64, len(weightedTests))
	totalWeight := uint64(0)
	for i, w := range weightedTests {
		tests[i] = w.Test
		weights[i] = w.Weight
		totalWeight += w.Weight
	}
	if err := weighted.Initialize(weights); err != nil {
		return RandomTest{}, err
	}

	return RandomTest{
		tests:       tests,
		weighted:    weighted,
		totalWeight: totalWeight,
	}, nil
}

func (r RandomTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	index, ok := r.weighted.Sample(rand.Uint64N(r.totalWeight)) //#nosec G404
	require.True(ok)

	r.tests[index].Run(tc, ctx, wallet)
}

type WeightedTest struct {
	Test   Test
	Weight uint64
}

type ZeroTransferTest struct{}

func (ZeroTransferTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	maxValue := int64(100 * 1_000_000_000 / params.TxGas)
	maxFeeCap := big.NewInt(maxValue)
	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)
	senderAddress := crypto.PubkeyToAddress(wallet.privKey.PublicKey)
	tx, err := types.SignNewTx(wallet.privKey, wallet.signer, &types.DynamicFeeTx{
		ChainID:   wallet.chainID,
		Nonce:     wallet.nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       params.TxGas,
		To:        &senderAddress,
		Data:      nil,
		Value:     common.Big0,
	})
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type ReadTest struct {
	Contract *contracts.EVMLoadSimulator
	Count    *big.Int
}

func (r ReadTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := r.Contract.SimulateReads(txOpts, r.Count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type WriteTest struct {
	Contract *contracts.EVMLoadSimulator
	Count    *big.Int
}

func (r WriteTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := r.Contract.SimulateRandomWrite(txOpts, r.Count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type StateModificationTest struct {
	Contract *contracts.EVMLoadSimulator
	Count    *big.Int
}

func (s StateModificationTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := s.Contract.SimulateModification(txOpts, s.Count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type HashingTest struct {
	Contract *contracts.EVMLoadSimulator
	Count    *big.Int
}

func (h HashingTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := h.Contract.SimulateHashing(txOpts, h.Count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type MemoryTest struct {
	Contract *contracts.EVMLoadSimulator
	Count    *big.Int
}

func (m MemoryTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := m.Contract.SimulateMemory(txOpts, m.Count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type CallDepthTest struct {
	Contract *contracts.EVMLoadSimulator
	Count    *big.Int
}

func (c CallDepthTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := c.Contract.SimulateCallDepth(txOpts, c.Count)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type ContractCreationTest struct {
	Contract *contracts.EVMLoadSimulator
}

func (c ContractCreationTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := c.Contract.SimulateContractCreation(txOpts)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type PureComputeTest struct {
	Contract      *contracts.EVMLoadSimulator
	NumIterations *big.Int
}

func (p PureComputeTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := p.Contract.SimulatePureCompute(txOpts, p.NumIterations)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type LargeEventTest struct {
	Contract  *contracts.EVMLoadSimulator
	NumEvents *big.Int
}

func (l LargeEventTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := l.Contract.SimulateLargeEvent(txOpts, l.NumEvents)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

type ExternalCallTest struct {
	Contract *contracts.EVMLoadSimulator
}

func (e ExternalCallTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := e.Contract.SimulateExternalCall(txOpts)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, time.Millisecond))
}

// newTxOpts returns transactions options for contract calls, with sending disabled
func newTxOpts(
	key *ecdsa.PrivateKey,
	chainID *big.Int,
	maxFeeCap *big.Int,
	nonce uint64,
) (*bind.TransactOpts, error) {
	txOpts, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, fmt.Errorf("creating transaction opts: %w", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	txOpts.GasFeeCap = maxFeeCap
	txOpts.GasTipCap = common.Big1
	txOpts.NoSend = true
	return txOpts, nil
}
