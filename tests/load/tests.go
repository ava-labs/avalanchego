// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"sync"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/load/contracts"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

var maxFeeCap = big.NewInt(300000000000)

// NewRandomTest creates a RandomWeightedTest containing a collection of EVM
// load testing scenarios.
//
// This function handles the setup of the tests and also assigns each test
// a weight based on its C-Chain frequency and computational intensity.
func NewRandomTest(
	ctx context.Context,
	chainID *big.Int,
	worker *Worker,
	source rand.Source,
	tokenContract *contracts.ERC20,
) (*RandomWeightedTest, error) {
	txOpts, err := bind.NewKeyedTransactorWithChainID(worker.PrivKey, chainID)
	if err != nil {
		return nil, err
	}

	_, tx, loadSimulator, err := contracts.DeployLoadSimulator(txOpts, worker.Client)
	if err != nil {
		return nil, err
	}

	if _, err := bind.WaitDeployed(ctx, worker.Client, tx); err != nil {
		return nil, err
	}

	worker.Nonce++

	_, tx, trieContract, err := contracts.DeployTrieStressTest(txOpts, worker.Client)
	if err != nil {
		return nil, err
	}

	if _, err := bind.WaitDeployed(ctx, worker.Client, tx); err != nil {
		return nil, err
	}

	worker.Nonce++

	var (
		// value specifies the amount to send in a transfer test
		value = big.NewInt(1)

		// random values are written to slots to ensure that the same value isn't
		// being written to a slot, which could reduce the expected gas used of a tx
		writeRand  = rand.New(rand.NewSource(0)) //#nosec G404
		modifyRand = rand.New(rand.NewSource(1)) //#nosec G404
	)

	weightedTests := []WeightedTest{
		{
			// minimum gas used: 21_000
			Test:   TransferTest{Value: value},
			Weight: 5,
		},
		{
			// minimum gas used: 84_000
			Test: ReadTest{
				Contract: loadSimulator,
				Offset:   big.NewInt(0),
				NumSlots: big.NewInt(30),
			},
			Weight: 10,
		},
		{
			// minimum gas used: 242_000
			Test: &WriteTest{
				Contract: loadSimulator,
				NumSlots: big.NewInt(10),
				Rand:     writeRand,
			},
			Weight: 10,
		},
		{
			// minimum gas used: 61_000
			Test: &ModifyTest{
				Contract: loadSimulator,
				NumSlots: big.NewInt(8),
				Rand:     modifyRand,
			},
			Weight: 10,
		},
		{
			// minimum gas used: 302_100
			Test: HashTest{
				Contract:      loadSimulator,
				Value:         big.NewInt(1),
				NumIterations: big.NewInt(1_000),
			},
			Weight: 5,
		},
		{
			// minimum gas used: 290_000
			Test:   DeployTest{Contract: loadSimulator},
			Weight: 5,
		},
		{
			// minimum gas used: 23_000
			Test: LargeCalldataTest{
				Contract: loadSimulator,
				Calldata: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			},
			Weight: 5,
		},
		{
			// minimum gas used: 155_900
			Test: TrieStressTest{
				Contract:  trieContract,
				NumValues: big.NewInt(12),
			},
			Weight: 10,
		},
		{
			// minimum gas used: 52_300
			Test: ERC20Test{
				Contract: tokenContract,
				Value:    value,
			},
			Weight: 45,
		},
	}

	return NewRandomWeightedTest(weightedTests, source)
}

type RandomWeightedTest struct {
	tests       []Test
	weighted    sampler.Weighted
	totalWeight int64

	mu   sync.Mutex
	rand *rand.Rand
}

func NewRandomWeightedTest(
	weightedTests []WeightedTest,
	source rand.Source,
) (*RandomWeightedTest, error) {
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
		return nil, err
	}

	if totalWeight > math.MaxInt64 {
		return nil, fmt.Errorf(
			"total weight larger than max int64, %d > %d",
			totalWeight,
			math.MaxInt64,
		)
	}

	rand := rand.New(source) //#nosec G404

	return &RandomWeightedTest{
		tests:       tests,
		weighted:    weighted,
		totalWeight: int64(totalWeight),
		rand:        rand,
	}, nil
}

func (r *RandomWeightedTest) Run(tc tests.TestContext, wallet *Wallet) {
	require := require.New(tc)

	r.mu.Lock()
	sampleValue := r.rand.Int63n(r.totalWeight)
	r.mu.Unlock()

	index, ok := r.weighted.Sample(uint64(sampleValue))
	require.True(ok)

	r.tests[index].Run(tc, wallet)
}

type WeightedTest struct {
	Test   Test
	Weight uint64
}

type TransferTest struct {
	Value *big.Int
}

func (t TransferTest) Run(tc tests.TestContext, wallet *Wallet) {
	require := require.New(tc)

	maxValue := int64(100 * 1_000_000_000 / params.TxGas)
	maxFeeCap := big.NewInt(maxValue)
	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)

	// Generate non-existent account address
	pk, err := crypto.GenerateKey()
	require.NoError(err)
	recipient := crypto.PubkeyToAddress(pk.PublicKey)

	tx, err := types.SignNewTx(wallet.privKey, wallet.signer, &types.DynamicFeeTx{
		ChainID:   wallet.chainID,
		Nonce:     wallet.nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       params.TxGas,
		To:        &recipient,
		Data:      nil,
		Value:     t.Value,
	})
	require.NoError(err)

	require.NoError(wallet.SendTx(tc.GetDefaultContextParent(), tx))
}

type ReadTest struct {
	Contract *contracts.LoadSimulator
	Offset   *big.Int
	NumSlots *big.Int
}

func (r ReadTest) Run(tc tests.TestContext, wallet *Wallet) {
	executeContractTx(tc, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return r.Contract.Read(txOpts, r.Offset, r.NumSlots)
	})
}

type WriteTest struct {
	Contract *contracts.LoadSimulator
	NumSlots *big.Int

	mu   sync.Mutex
	Rand *rand.Rand
}

func (w *WriteTest) Run(tc tests.TestContext, wallet *Wallet) {
	w.mu.Lock()
	value := w.Rand.Int63()
	w.mu.Unlock()

	executeContractTx(tc, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return w.Contract.Write(txOpts, w.NumSlots, big.NewInt(value))
	})
}

type ModifyTest struct {
	Contract *contracts.LoadSimulator
	NumSlots *big.Int

	mu   sync.Mutex
	Rand *rand.Rand
}

func (m *ModifyTest) Run(tc tests.TestContext, wallet *Wallet) {
	m.mu.Lock()
	value := m.Rand.Int63()
	m.mu.Unlock()

	executeContractTx(tc, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return m.Contract.Modify(txOpts, m.NumSlots, big.NewInt(value))
	})
}

type HashTest struct {
	Contract      *contracts.LoadSimulator
	Value         *big.Int
	NumIterations *big.Int
}

func (h HashTest) Run(tc tests.TestContext, wallet *Wallet) {
	executeContractTx(tc, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return h.Contract.Hash(txOpts, h.Value, h.NumIterations)
	})
}

type DeployTest struct {
	Contract *contracts.LoadSimulator
}

func (d DeployTest) Run(tc tests.TestContext, wallet *Wallet) {
	executeContractTx(tc, wallet, d.Contract.Deploy)
}

type LargeCalldataTest struct {
	Contract *contracts.LoadSimulator
	Calldata []byte
}

func (l LargeCalldataTest) Run(tc tests.TestContext, wallet *Wallet) {
	executeContractTx(tc, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return l.Contract.LargeCalldata(txOpts, l.Calldata)
	})
}

type TrieStressTest struct {
	Contract  *contracts.TrieStressTest
	NumValues *big.Int
}

func (t TrieStressTest) Run(tc tests.TestContext, wallet *Wallet) {
	executeContractTx(tc, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return t.Contract.WriteValues(txOpts, t.NumValues)
	})
}

type ERC20Test struct {
	Contract *contracts.ERC20
	Value    *big.Int
}

func (e ERC20Test) Run(tc tests.TestContext, wallet *Wallet) {
	require := require.New(tc)

	// Generate non-existent account address
	pk, err := crypto.GenerateKey()
	require.NoError(err)
	recipient := crypto.PubkeyToAddress(pk.PublicKey)

	executeContractTx(tc, wallet, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
		return e.Contract.Transfer(txOpts, recipient, e.Value)
	})
}

func executeContractTx(
	tc tests.TestContext,
	wallet *Wallet,
	txFunc func(*bind.TransactOpts) (*types.Transaction, error),
) {
	require := require.New(tc)

	txOpts, err := newTxOpts(wallet.privKey, wallet.chainID, maxFeeCap, wallet.nonce)
	require.NoError(err)

	tx, err := txFunc(txOpts)
	require.NoError(err)

	require.NoError(wallet.SendTx(tc.GetDefaultContextParent(), tx))
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
