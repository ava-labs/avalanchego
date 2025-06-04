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
)

var (
	errZeroTotalWeight      = errors.New("total weight is zero")
	errFailedToSelectTxType = errors.New("failed to select tx type")

	maxFeeCap = big.NewInt(300000000000)
)

func BuildZeroTransferTx(backend Backend) (*types.Transaction, error) {
	maxValue := int64(100 * 1_000_000_000 / params.TxGas)
	maxFeeCap := big.NewInt(maxValue)
	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)
	senderAddress := crypto.PubkeyToAddress(backend.PrivKey().PublicKey)
	tx, err := types.SignNewTx(backend.PrivKey(), backend.Signer(), &types.DynamicFeeTx{
		ChainID:   backend.ChainID(),
		Nonce:     backend.Nonce(),
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

func BuildRandomWriteTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxWriteSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxWriteSizeBytes)) //#nosec G404
	return contractInstance.SimulateRandomWrite(txOpts, count)
}

func BuildStateModificationTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxStateSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxStateSizeBytes)) //#nosec G404
	return contractInstance.SimulateModification(txOpts, count)
}

func BuildRandomReadTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxReadSizeBytes = 5
	count := big.NewInt(rand.Int64N(maxReadSizeBytes)) //#nosec G404
	return contractInstance.SimulateReads(txOpts, count)
}

func BuildHashingTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxRounds = 3
	count := big.NewInt(rand.Int64N(maxRounds)) //#nosec G404
	return contractInstance.SimulateHashing(txOpts, count)
}

func BuildMemoryTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxRounds = 3
	count := big.NewInt(rand.Int64N(maxRounds)) //#nosec G404
	return contractInstance.SimulateMemory(txOpts, count)
}

func BuildCallDepthTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxDepth = 5
	count := big.NewInt(rand.Int64N(maxDepth)) //#nosec G404
	return contractInstance.SimulateCallDepth(txOpts, count)
}

func BuildContractCreationTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	return contractInstance.SimulateContractCreation(txOpts)
}

func BuildPureComputeTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const iterations = 100
	return contractInstance.SimulatePureCompute(txOpts, big.NewInt(iterations))
}

func BuildLargeEventTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	const maxEventSize = 100
	return contractInstance.SimulateLargeEvent(txOpts, big.NewInt(maxEventSize))
}

func BuildExternalCallTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txOpts, err := newTxOpts(
		backend.PrivKey(),
		backend.ChainID(),
		maxFeeCap,
		backend.Nonce(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx opts: %w", err)
	}
	return contractInstance.SimulateExternalCall(txOpts)
}

func BuildRandomTx(
	backend Backend,
	contractInstance *contracts.EVMLoadSimulator,
) (*types.Transaction, error) {
	txTypes := []txType{
		{
			txFunc: func(b Backend, _ *contracts.EVMLoadSimulator) (*types.Transaction, error) {
				return BuildZeroTransferTx(b)
			},
			name:   "ZeroTransfer",
			weight: 1000,
		},
		{
			txFunc: BuildRandomReadTx,
			name:   "RandomWrite",
			weight: 100,
		},
		{
			txFunc: BuildStateModificationTx,
			name:   "StateModification",
			weight: 100,
		},
		{
			txFunc: BuildRandomReadTx,
			name:   "RandomRead",
			weight: 200,
		},
		{
			txFunc: BuildHashingTx,
			name:   "Hashing",
			weight: 50,
		},
		{
			txFunc: BuildMemoryTx,
			name:   "Memory",
			weight: 100,
		},
		{
			txFunc: BuildCallDepthTx,
			name:   "CallDepth",
			weight: 50,
		},
		{
			txFunc: BuildContractCreationTx,
			name:   "ContractCreation",
			weight: 1,
		},
		{
			txFunc: BuildPureComputeTx,
			name:   "PureCompute",
			weight: 100,
		},
		{
			txFunc: BuildLargeEventTx,
			name:   "LargeEvent",
			weight: 100,
		},
		{
			txFunc: BuildExternalCallTx,
			name:   "ExternalCall",
			weight: 50,
		},
	}

	txType, err := pickWeightedRandom(txTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to select random tx: %w", err)
	}

	tx, err := txType.txFunc(backend, contractInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tx of type %s: %w", txType.name, err)
	}

	return tx, nil
}

type txType struct {
	txFunc func(Backend, *contracts.EVMLoadSimulator) (*types.Transaction, error)
	name   string
	weight uint
}

func pickWeightedRandom(txTypes []txType) (txType, error) {
	var totalWeight uint
	for _, txType := range txTypes {
		totalWeight += txType.weight
	}

	// this ensures that UintN does not panic
	if totalWeight == 0 {
		return txType{}, errZeroTotalWeight
	}

	r := rand.UintN(totalWeight) //nolint:gosec

	for _, txType := range txTypes {
		if r < txType.weight {
			return txType, nil
		}
		r -= txType.weight
	}
	return txType{}, errFailedToSelectTxType
}

func newTxOpts(
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
