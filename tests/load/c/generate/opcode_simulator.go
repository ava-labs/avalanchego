// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package generate

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"math/rand"

	"github.com/ava-labs/avalanchego/tests/load/agent"
	evmloadsimulator "github.com/ava-labs/avalanchego/tests/load/c/contracts/abi-bindings"
	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	ethcrypto "github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/rpc"
)

var _ agent.TxGenerator[*types.Transaction] = (*Self)(nil)

// Enum representing the simulator external function types
type SimulatedLoadType int

const (
	RandomWrite SimulatedLoadType = iota
	StateModification
	RandomReads
	Hashing
	Memory
	CallDepth
	ContractCreation
)

// OpCodeSimulator generates transactions that randomly call the external functions
// of a EVMLoadSumulator contract instance that it deploys.
type OpCodeSimulator struct {
	senderKey                 *ecdsa.PrivateKey
	simulatorContractAddress  common.Address
	simulatorContractInstance *evmloadsimulator.EVMLoadSimulator
	txOpts                    *bind.TransactOpts
	client                    *ethclient.Client
}

func NewOpCodeSimulator(
	ctx context.Context,
	client *ethclient.Client,
	maxTipCap *big.Int,
	maxFeeCap *big.Int,
	key *ecdsa.PrivateKey,
) (*OpCodeSimulator, error) {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	nonce, err := client.NonceAt(ctx, ethcrypto.PubkeyToAddress(key.PublicKey), big.NewInt(int64(rpc.PendingBlockNumber)))
	if err != nil {
		return nil, fmt.Errorf("getting nonce: %w", err)
	}

	// Create bind.TransactionOpts
	opts, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return nil, fmt.Errorf("creating transaction opts: %w", err)
	}
	opts.Nonce = big.NewInt(int64(nonce))
	opts.GasFeeCap = maxFeeCap
	opts.GasTipCap = maxTipCap

	_, simulatorDeploymentTx, simulatorInstance, err := evmloadsimulator.DeployEVMLoadSimulator(opts, client)
	if err != nil {
		return nil, fmt.Errorf("deploying simulator contract: %w", err)
	}
	opts.Nonce = new(big.Int).Add(opts.Nonce, big.NewInt(1))

	// Wait for the contract to be mined
	simulatorAddress, err := bind.WaitDeployed(ctx, client, simulatorDeploymentTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for simulator contract to be mined: %w", err)
	}

	return &OpCodeSimulator{
		senderKey:                 key,
		txOpts:                    opts,
		client:                    client,
		simulatorContractAddress:  simulatorAddress,
		simulatorContractInstance: simulatorInstance,
	}, nil
}

func (ocs *OpCodeSimulator) GenerateTx() (*types.Transaction, error) {
	// Generate a random simulated load type
	loadType := SimulatedLoadType(rand.Intn(int(ContractCreation) + 1))
	var res *types.Transaction
	var err error

	// TODO: Currently, the transactor calls will both construct and issue the transaction,
	// but ideally we only want to construct the transaction and return it here without issuing it.
	switch loadType {
	case RandomWrite:
		maxWriteSizeBytes := int64(5)
		writeSize := big.NewInt(rand.Int63n(maxWriteSizeBytes))
		res, err = ocs.simulatorContractInstance.SimulateRandomWrite(ocs.txOpts, writeSize)
	case StateModification:
		maxStateSizeBytes := int64(5)
		stateSize := big.NewInt(rand.Int63n(maxStateSizeBytes))
		res, err = ocs.simulatorContractInstance.SimulateModification(ocs.txOpts, stateSize)
	case RandomReads:
		maxReadSizeBytes := int64(5)
		numReads := big.NewInt(rand.Int63n(maxReadSizeBytes))
		res, err = ocs.simulatorContractInstance.SimulateReads(ocs.txOpts, numReads)
	case Hashing:
		maxRounds := int64(3)
		rounds := big.NewInt(rand.Int63n(maxRounds))
		res, err = ocs.simulatorContractInstance.SimulateHashing(ocs.txOpts, rounds)
	case Memory:
		maxArraySize := int64(4)
		arraySize := big.NewInt(rand.Int63n(maxArraySize))
		res, err = ocs.simulatorContractInstance.SimulateMemory(ocs.txOpts, arraySize)
	case CallDepth:
		maxDepth := int64(5)
		depth := big.NewInt(rand.Int63n(maxDepth))
		res, err = ocs.simulatorContractInstance.SimulateCallDepth(ocs.txOpts, depth)
	case ContractCreation:
		res, err = ocs.simulatorContractInstance.SimulateContractCreation(ocs.txOpts)
	default:
		return nil, fmt.Errorf("invalid simulated load type: %d", loadType)
	}

	if err != nil {
		return nil, fmt.Errorf("calling simulator contract: %w", err)
	}

	ocs.txOpts.Nonce = new(big.Int).Add(ocs.txOpts.Nonce, big.NewInt(1))
	return res, err
}
