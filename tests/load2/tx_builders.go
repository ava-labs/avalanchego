// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/tests/load/c/contracts"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

var (
	maxFeeCap           = big.NewInt(300000000000)
	errFailedToSelectTx = errors.New("failed to select random tx")
)

type RandomTxBuilder struct {
	builders    []txType
	sampler     sampler.Weighted
	totalWeight uint64
}

func NewRandomTxBuilder(contract *contracts.EVMLoadSimulator) (RandomTxBuilder, error) {
	builders := []txType{
		{
			builder: zeroTransferTxBuilder{},
			weight:  1000,
		},
		{
			builder: randomWriteTxBuilder{contract: contract},
			weight:  100,
		},
		{
			builder: stateModificationTxBuilder{contract: contract},
			weight:  100,
		},
		{
			builder: randomReadTxBuilder{contract: contract},
			weight:  200,
		},
		{
			builder: hashingTxBuilder{contract: contract},
			weight:  50,
		},
		{
			builder: memoryTxBuilder{contract: contract},
			weight:  100,
		},
		{
			builder: callDepthTxBuilder{contract: contract},
			weight:  50,
		},
		{
			builder: contractCreationTxBuilder{contract: contract},
			weight:  1,
		},
		{
			builder: pureComputeTxBuilder{contract: contract},
			weight:  100,
		},
		{
			builder: largeEventTxBuilder{contract: contract},
			weight:  100,
		},
		{
			builder: externalCallTxBuilder{contract: contract},
			weight:  50,
		},
	}

	// define weights and sampler
	weights := make([]uint64, len(builders))
	totalWeight := uint64(0)
	for i, builder := range builders {
		weights[i] = builder.weight
		totalWeight += builder.weight
	}

	weightedSampler := sampler.NewWeighted()
	if err := weightedSampler.Initialize(weights); err != nil {
		return RandomTxBuilder{}, fmt.Errorf("failed to initialize sampler: %w", err)
	}

	return RandomTxBuilder{
		builders:    builders,
		sampler:     weightedSampler,
		totalWeight: totalWeight,
	}, nil
}

func (r RandomTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	index, ok := r.sampler.Sample(rand.Uint64N(r.totalWeight)) //#nosec G404
	if !ok {
		return nil, errFailedToSelectTx
	}

	selectedBuilder := r.builders[index]
	tx, err := selectedBuilder.builder.Build(wallet)
	if err != nil {
		return nil, fmt.Errorf("failed to build tx with builder %T: %w", selectedBuilder, err)
	}

	return tx, nil
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
	builder TxBuilder
	weight  uint64
}

type zeroTransferTxBuilder struct{}

func (zeroTransferTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
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
	if err != nil {
		return nil, fmt.Errorf("failed to sign tx: %w", err)
	}
	return tx, nil
}

type randomWriteTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (r randomWriteTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxWriteSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxWriteSizeBytes)) //#nosec G404
	return r.contract.SimulateRandomWrite(txOpts, count)
}

type stateModificationTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (s stateModificationTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxStateSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxStateSizeBytes)) //#nosec G404
	return s.contract.SimulateModification(txOpts, count)
}

type randomReadTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (r randomReadTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxReadSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxReadSizeBytes)) //#nosec G404
	return r.contract.SimulateReads(txOpts, count)
}

type hashingTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (h hashingTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxRounds = 3
	count := big.NewInt(rand.Int64N(maxRounds)) //#nosec G404
	return h.contract.SimulateHashing(txOpts, count)
}

type memoryTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (m memoryTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxRounds = 3
	count := big.NewInt(rand.Int64N(maxRounds)) //#nosec G404
	return m.contract.SimulateMemory(txOpts, count)
}

type callDepthTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (c callDepthTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxDepth = 5
	count := big.NewInt(rand.Int64N(maxDepth)) //#nosec G404
	return c.contract.SimulateCallDepth(txOpts, count)
}

type contractCreationTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (c contractCreationTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	return c.contract.SimulateContractCreation(txOpts)
}

type pureComputeTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (p pureComputeTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const iterations = 100
	return p.contract.SimulatePureCompute(txOpts, big.NewInt(iterations))
}

type largeEventTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (l largeEventTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxEventSize = 100
	return l.contract.SimulateLargeEvent(txOpts, big.NewInt(maxEventSize))
}

type externalCallTxBuilder struct {
	contract *contracts.EVMLoadSimulator
}

func (e externalCallTxBuilder) Build(wallet *Wallet) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	return e.contract.SimulateExternalCall(txOpts)
}
