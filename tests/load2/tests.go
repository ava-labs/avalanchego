// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"crypto/ecdsa"
	"fmt"
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

// NewRandomTest creates a RandomWeightedTest containing a collection of EVM
// load testing scenarios.
//
// This function handles the setup of the tests and also assigns each test
// an equal weight, making them equally likely to be selected during random test execution.
func NewRandomTest(ctx context.Context, chainID *big.Int, worker *Worker) (RandomWeightedTest, error) {
	txOpts, err := bind.NewKeyedTransactorWithChainID(worker.PrivKey, chainID)
	if err != nil {
		return RandomWeightedTest{}, err
	}

	_, tx, contract, err := contracts.DeployEVMLoadSimulator(txOpts, worker.Client)
	if err != nil {
		return RandomWeightedTest{}, err
	}

	if _, err := bind.WaitDeployed(ctx, worker.Client, tx); err != nil {
		return RandomWeightedTest{}, err
	}

	worker.Nonce++

	weight := uint64(100)
	count := big.NewInt(5)
	weightedTests := []WeightedTest{
		{
			Test:   ZeroTransferTest{},
			Weight: weight,
		},
		{
			Test: ReadTest{
				Contract: contract,
				Count:    count,
			},
			Weight: weight,
		},
		{
			Test: WriteTest{
				Contract: contract,
				Count:    count,
			},
			Weight: weight,
		},
		{
			Test: StateModificationTest{
				Contract: contract,
				Count:    count,
			},
			Weight: weight,
		},
		{
			Test: HashingTest{
				Contract: contract,
				Count:    count,
			},
			Weight: weight,
		},
		{
			Test: MemoryTest{
				Contract: contract,
				Count:    count,
			},
			Weight: weight,
		},
		{
			Test: CallDepthTest{
				Contract: contract,
				Count:    count,
			},
			Weight: weight,
		},
		{
			Test:   ContractCreationTest{Contract: contract},
			Weight: weight,
		},
		{
			Test: PureComputeTest{
				Contract:      contract,
				NumIterations: count,
			},
			Weight: weight,
		},
		{
			Test: LargeEventTest{
				Contract:  contract,
				NumEvents: count,
			},
			Weight: weight,
		},
		{
			Test:   ExternalCallTest{Contract: contract},
			Weight: weight,
		},
	}

	return NewRandomWeightedTest(weightedTests)
}

type RandomWeightedTest struct {
	tests       []Test
	weighted    sampler.Weighted
	totalWeight uint64
}

func NewRandomWeightedTest(weightedTests []WeightedTest) (RandomWeightedTest, error) {
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
		return RandomWeightedTest{}, err
	}

	return RandomWeightedTest{
		tests:       tests,
		weighted:    weighted,
		totalWeight: totalWeight,
	}, nil
}

func (r RandomWeightedTest) Run(
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

	require.NoError(wallet.SendTx(ctx, tx))
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
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return r.Contract.SimulateReads(txOpts, r.Count)
	})
}

type WriteTest struct {
	Contract *contracts.EVMLoadSimulator
	Count    *big.Int
}

func (w WriteTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return w.Contract.SimulateRandomWrite(txOpts, w.Count)
	})
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
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return s.Contract.SimulateModification(txOpts, s.Count)
	})
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
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return h.Contract.SimulateHashing(txOpts, h.Count)
	})
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
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return m.Contract.SimulateMemory(txOpts, m.Count)
	})
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
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return c.Contract.SimulateCallDepth(txOpts, c.Count)
	})
}

type ContractCreationTest struct {
	Contract *contracts.EVMLoadSimulator
}

func (c ContractCreationTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	executeContractTx(tc, ctx, wallet, c.Contract.SimulateContractCreation)
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
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return p.Contract.SimulatePureCompute(txOpts, p.NumIterations)
	})
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
	executeContractTx(tc, ctx, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return l.Contract.SimulateLargeEvent(txOpts, l.NumEvents)
	})
}

type ExternalCallTest struct {
	Contract *contracts.EVMLoadSimulator
}

func (e ExternalCallTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	executeContractTx(tc, ctx, wallet, e.Contract.SimulateExternalCall)
}

func executeContractTx(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
	txFunc func(*bind.TransactOpts) (*types.Transaction, error),
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := txFunc(txOpts)
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx))
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
		return nil, fmt.Errorf("failed to create transaction opts: %w", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	txOpts.GasFeeCap = maxFeeCap
	txOpts.GasTipCap = common.Big1
	txOpts.NoSend = true
	return txOpts, nil
}
