// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"math/rand/v2"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/tests/load/c/contracts"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type EthClient interface {
	ChainID(ctx context.Context) (*big.Int, error)
	EthClientSender
	bind.DeployBackend
	bind.ContractBackend
}

type EthClientSender interface {
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

type IssueTracker interface {
	Issue(tx common.Hash)
}

// issuer generates and issues transactions that randomly call the
// external functions of the [contracts.EVMLoadSimulator] contract
// instance that it deploys.
type issuer struct {
	// Injected parameters
	tracker   IssueTracker
	maxFeeCap *big.Int

	// Determined by constructor
	txTypes []txType

	// State
	nonce uint64
}

func createIssuer(
	ctx context.Context,
	client EthClient,
	tracker IssueTracker,
	nonce uint64,
	maxFeeCap *big.Int,
	key *ecdsa.PrivateKey,
) (*issuer, error) {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	maxTipCap := big.NewInt(1)
	txOpts, err := newTxOpts(ctx, key, chainID, maxFeeCap, maxTipCap, nonce)
	if err != nil {
		return nil, fmt.Errorf("creating transaction opts: %w", err)
	}
	_, simulatorDeploymentTx, simulatorInstance, err := contracts.DeployEVMLoadSimulator(txOpts, client)
	if err != nil {
		return nil, fmt.Errorf("deploying simulator contract: %w", err)
	}
	nonce++ // deploying contract consumes one nonce

	_, err = bind.WaitDeployed(ctx, client, simulatorDeploymentTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for simulator contract to be mined: %w", err)
	}

	return &issuer{
		tracker: tracker,
		txTypes: makeTxTypes(simulatorInstance, key, chainID, client),
		nonce:   nonce,
	}, nil
}

func (o *issuer) GenerateAndIssueTx(ctx context.Context) (common.Hash, error) {
	txType := pickWeightedRandom(o.txTypes)

	tx, err := txType.generateAndIssueTx(ctx, o.maxFeeCap, o.nonce)
	if err != nil {
		return common.Hash{}, fmt.Errorf("generating and issuing transaction of type %s: %w", txType.name, err)
	}

	o.nonce++
	txHash := tx.Hash()
	o.tracker.Issue(txHash)
	return txHash, err
}

func makeTxTypes(contractInstance *contracts.EVMLoadSimulator, senderKey *ecdsa.PrivateKey,
	chainID *big.Int, client EthClientSender,
) []txType {
	senderAddress := ethcrypto.PubkeyToAddress(senderKey.PublicKey)
	signer := types.LatestSignerForChainID(chainID)
	return []txType{
		{
			name:   "zero self transfer",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				bigGwei := big.NewInt(params.GWei)
				gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
				gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)
				tx, err := types.SignNewTx(senderKey, signer, &types.DynamicFeeTx{
					ChainID:   chainID,
					Nonce:     nonce,
					GasTipCap: gasTipCap,
					GasFeeCap: gasFeeCap,
					Gas:       params.TxGas,
					To:        &senderAddress,
					Data:      nil,
					Value:     common.Big0,
				})
				if err != nil {
					return nil, fmt.Errorf("signing transaction: %w", err)
				}
				if err := client.SendTransaction(txCtx, tx); err != nil {
					return nil, fmt.Errorf("issuing transaction with nonce %d: %w", nonce, err)
				}
				return tx, nil
			},
		},
		{
			name:   "random write",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxWriteSizeBytes = 5
				return contractInstance.SimulateRandomWrite(txOpts, intNBigInt(maxWriteSizeBytes))
			},
		},
		{
			name:   "state modification",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxStateSizeBytes = 5
				return contractInstance.SimulateModification(txOpts, intNBigInt(maxStateSizeBytes))
			},
		},
		{
			name:   "random read",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxReadSizeBytes = 5
				return contractInstance.SimulateReads(txOpts, intNBigInt(maxReadSizeBytes))
			},
		},
		{
			name:   "hashing",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxRounds = 3
				return contractInstance.SimulateHashing(txOpts, intNBigInt(maxRounds))
			},
		},
		{
			name:   "memory",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxArraySize = 4
				return contractInstance.SimulateMemory(txOpts, intNBigInt(maxArraySize))
			},
		},
		{
			name:   "call depth",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxDepth = 5
				return contractInstance.SimulateCallDepth(txOpts, intNBigInt(maxDepth))
			},
		},
		{
			name:   "contract creation",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				return contractInstance.SimulateContractCreation(txOpts)
			},
		},
		{
			name:   "pure compute",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const iterations = 100
				return contractInstance.SimulatePureCompute(txOpts, big.NewInt(iterations))
			},
		},
		{
			name:   "large event",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxEventSize = 100
				return contractInstance.SimulateLargeEvent(txOpts, big.NewInt(maxEventSize))
			},
		},
		{
			name:   "external call",
			weight: 1,
			generateAndIssueTx: func(txCtx context.Context, maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(txCtx, senderKey, chainID, maxFeeCap, big.NewInt(1), nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				return contractInstance.SimulateExternalCall(txOpts)
			},
		},
	}
}

type txType struct {
	name               string // for error wrapping only
	weight             uint
	generateAndIssueTx func(txCtx context.Context, gasFeeCap *big.Int, nonce uint64) (*types.Transaction, error)
}

func pickWeightedRandom(txTypes []txType) txType {
	var totalWeight uint
	for _, txType := range txTypes {
		totalWeight += txType.weight
	}

	if totalWeight == 0 {
		panic("Total weight cannot be zero")
	}

	r := rand.UintN(totalWeight) //nolint:gosec

	for _, txType := range txTypes {
		if r < txType.weight {
			return txType
		}
		r -= txType.weight
	}
	panic("failed to pick a tx type")
}

func intNBigInt(n int64) *big.Int {
	if n <= 0 {
		panic("n must be greater than 0")
	}
	return big.NewInt(rand.Int64N(n)) //nolint:gosec
}

func newTxOpts(ctx context.Context, key *ecdsa.PrivateKey,
	chainID, maxFeeCap, maxTipCap *big.Int, nonce uint64,
) (*bind.TransactOpts, error) {
	txOpts, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, fmt.Errorf("creating transaction opts: %w", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	txOpts.GasFeeCap = maxFeeCap
	txOpts.GasTipCap = maxTipCap
	txOpts.Context = ctx
	return txOpts, nil
}
