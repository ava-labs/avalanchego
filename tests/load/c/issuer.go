// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/tests/load/c/contracts"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

var (
	errNonpositiveSeed      = errors.New("seed is nonpositive")
	errZeroTotalWeight      = errors.New("total weight is zero")
	errFailedToSelectTxType = errors.New("failed to select tx type")
)

// Issuer generates and issues transactions that randomly call the
// external functions of the [contracts.EVMLoadSimulator] contract
// instance that it deploys.
type Issuer struct {
	// Determined by constructor
	txTypes []txType

	// State
	nonce uint64
}

func NewIssuer(
	ctx context.Context,
	client *ethclient.Client,
	nonce uint64,
	key *ecdsa.PrivateKey,
) (*Issuer, error) {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	maxFeeCap := big.NewInt(300000000000) // enough for contract deployment in parallel
	txOpts, err := newTxOpts(key, chainID, maxFeeCap, nonce)
	if err != nil {
		return nil, fmt.Errorf("creating transaction opts: %w", err)
	}
	// add params necessary for tx issuance
	txOpts.NoSend = false
	txOpts.Context = ctx

	_, simulatorDeploymentTx, simulatorInstance, err := contracts.DeployEVMLoadSimulator(txOpts, client)
	if err != nil {
		return nil, fmt.Errorf("deploying simulator contract: %w", err)
	}
	nonce++ // deploying contract consumes one nonce

	_, err = bind.WaitDeployed(ctx, client, simulatorDeploymentTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for simulator contract to be mined: %w", err)
	}

	return &Issuer{
		txTypes: makeTxTypes(simulatorInstance, key, chainID),
		nonce:   nonce,
	}, nil
}

func (i *Issuer) BuildTx() (*types.Transaction, error) {
	txType, err := pickWeightedRandom(i.txTypes)
	if err != nil {
		return nil, err
	}

	tx, err := txType.generate(txType.maxFeeCap, i.nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to generate transaction of type %s: %w", txType.name, err)
	}

	i.nonce++

	return tx, nil
}

func makeTxTypes(
	contractInstance *contracts.EVMLoadSimulator,
	senderKey *ecdsa.PrivateKey,
	chainID *big.Int,
) []txType {
	senderAddress := ethcrypto.PubkeyToAddress(senderKey.PublicKey)
	signer := types.LatestSignerForChainID(chainID)
	return []txType{
		{
			name:      "zero self transfer",
			weight:    1000,
			maxFeeCap: big.NewInt(4761904), // equiavelent to 100 ETH which is the maximum value
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
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
				return tx, nil
			},
		},
		{
			name:      "random write",
			weight:    100,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxWriteSizeBytes = 5
				count, err := randomNum(maxWriteSizeBytes)
				if err != nil {
					return nil, err
				}
				return contractInstance.SimulateRandomWrite(txOpts, count)
			},
		},
		{
			name:      "state modification",
			weight:    100,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxStateSizeBytes = 5
				count, err := randomNum(maxStateSizeBytes)
				if err != nil {
					return nil, err
				}
				return contractInstance.SimulateModification(txOpts, count)
			},
		},
		{
			name:      "random read",
			weight:    200,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxReadSizeBytes = 5
				count, err := randomNum(maxReadSizeBytes)
				if err != nil {
					return nil, err
				}
				return contractInstance.SimulateReads(txOpts, count)
			},
		},
		{
			name:      "hashing",
			weight:    50,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxRounds = 3
				count, err := randomNum(maxRounds)
				if err != nil {
					return nil, err
				}
				return contractInstance.SimulateHashing(txOpts, count)
			},
		},
		{
			name:      "memory",
			weight:    100,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxArraySize = 4
				count, err := randomNum(maxArraySize)
				if err != nil {
					return nil, err
				}
				return contractInstance.SimulateMemory(txOpts, count)
			},
		},
		{
			name:      "call depth",
			weight:    50,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxDepth = 5
				count, err := randomNum(maxDepth)
				if err != nil {
					return nil, err
				}
				return contractInstance.SimulateCallDepth(txOpts, count)
			},
		},
		{
			name:      "contract creation",
			weight:    1,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				return contractInstance.SimulateContractCreation(txOpts)
			},
		},
		{
			name:      "pure compute",
			weight:    100,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const iterations = 100
				return contractInstance.SimulatePureCompute(txOpts, big.NewInt(iterations))
			},
		},
		{
			name:      "large event",
			weight:    100,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				const maxEventSize = 100
				return contractInstance.SimulateLargeEvent(txOpts, big.NewInt(maxEventSize))
			},
		},
		{
			name:      "external call",
			weight:    50,
			maxFeeCap: big.NewInt(300000000000),
			generate: func(maxFeeCap *big.Int, nonce uint64) (*types.Transaction, error) {
				txOpts, err := newTxOpts(senderKey, chainID, maxFeeCap, nonce)
				if err != nil {
					return nil, fmt.Errorf("creating transaction opts: %w", err)
				}
				return contractInstance.SimulateExternalCall(txOpts)
			},
		},
	}
}

type txType struct {
	name      string // for error wrapping only
	weight    uint
	maxFeeCap *big.Int
	generate  func(gasFeeCap *big.Int, nonce uint64) (*types.Transaction, error)
}

func pickWeightedRandom(txTypes []txType) (txType, error) {
	var totalWeight uint
	for _, txType := range txTypes {
		totalWeight += txType.weight
	}

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

func randomNum(seed int64) (*big.Int, error) {
	if seed <= 0 {
		return nil, errNonpositiveSeed
	}
	return big.NewInt(rand.Int64N(seed)), nil //nolint:gosec
}

// newTxOpts returns transaction options for contract calls, with transaction
// issuance disabled
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
