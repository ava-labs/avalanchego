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

var maxFeeCap = big.NewInt(300000000000)

func BuildRandomTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txTypes := []txType{
		{
			txFunc: func(w *Wallet, _ *contracts.EVMLoadSimulator) (*types.Transaction, error) {
				return buildZeroTransferTx(w)
			},
			name:   "ZeroTransfer",
			weight: 1000,
		},
		{
			txFunc: buildRandomWriteTx,
			name:   "RandomWrite",
			weight: 100,
		},
		{
			txFunc: buildStateModificationTx,
			name:   "StateModification",
			weight: 100,
		},
		{
			txFunc: buildRandomReadTx,
			name:   "RandomRead",
			weight: 200,
		},
		{
			txFunc: buildHashingTx,
			name:   "Hashing",
			weight: 50,
		},
		{
			txFunc: buildMemoryTx,
			name:   "Memory",
			weight: 100,
		},
		{
			txFunc: buildCallDepthTx,
			name:   "CallDepth",
			weight: 50,
		},
		{
			txFunc: buildContractCreationTx,
			name:   "ContractCreation",
			weight: 1,
		},
		{
			txFunc: buildPureComputeTx,
			name:   "PureCompute",
			weight: 100,
		},
		{
			txFunc: buildLargeEventTx,
			name:   "LargeEvent",
			weight: 100,
		},
		{
			txFunc: buildExternalCallTx,
			name:   "ExternalCall",
			weight: 50,
		},
	}

	weights := make([]uint64, len(txTypes))
	totalWeight := uint64(0)
	for i, txType := range txTypes {
		weights[i] = txType.weight
		totalWeight += txType.weight
	}

	sampler := sampler.NewWeighted()
	if err := sampler.Initialize(weights); err != nil {
		return nil, fmt.Errorf("failed to initialize sampler: %w", err)
	}

	index, ok := sampler.Sample(rand.Uint64N(totalWeight)) //#nosec G404
	if !ok {
		return nil, errors.New("failed to select random tx")
	}

	txType := txTypes[index]
	tx, err := txType.txFunc(wallet, contractInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tx of type %s: %w", txType.name, err)
	}

	return tx, nil
}

func WithContractInstance(
	f func(*Wallet, *contracts.EVMLoadSimulator) (*types.Transaction, error),
	contractInstance *contracts.EVMLoadSimulator,
) func(*Wallet) (*types.Transaction, error) {
	return func(w *Wallet) (*types.Transaction, error) {
		return f(w, contractInstance)
	}
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

func buildZeroTransferTx(wallet *Wallet) (*types.Transaction, error) {
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

func buildRandomWriteTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulateRandomWrite(txOpts, count)
}

func buildStateModificationTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulateModification(txOpts, count)
}

func buildRandomReadTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulateReads(txOpts, count)
}

func buildHashingTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulateHashing(txOpts, count)
}

func buildMemoryTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulateMemory(txOpts, count)
}

func buildCallDepthTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulateCallDepth(txOpts, count)
}

func buildContractCreationTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	return contractInstance.SimulateContractCreation(txOpts)
}

func buildPureComputeTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulatePureCompute(txOpts, big.NewInt(iterations))
}

func buildLargeEventTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
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
	return contractInstance.SimulateLargeEvent(txOpts, big.NewInt(maxEventSize))
}

func buildExternalCallTx(
	wallet *Wallet,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := NewTxOpts(
		wallet.PrivKey(),
		wallet.ChainID(),
		maxFeeCap,
		wallet.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	return contractInstance.SimulateExternalCall(txOpts)
}

type txType struct {
	txFunc func(*Wallet, *contracts.EVMLoadSimulator) (*types.Transaction, error)
	name   string
	weight uint64
}
