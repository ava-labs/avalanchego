// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuers

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
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/tests/load/c/contracts"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type EthClientOpcoder interface {
	EthClientSimple
	bind.DeployBackend
	bind.ContractBackend
}

// Opcoder generates and issues transactions that randomly call the
// external functions of the [contracts.EVMLoadSimulator] contract
// instance that it deploys.
type Opcoder struct {
	// Injected parameters
	client      EthClientOpcoder
	tracker     IssueTracker
	senderKey   *ecdsa.PrivateKey
	maxFeeCap   *big.Int
	issuePeriod time.Duration

	// Determined by constructor
	chainID          *big.Int
	maxTipCap        *big.Int
	contractAddress  common.Address
	contractInstance *contracts.EVMLoadSimulator

	// State
	nonce     uint64
	lastIssue time.Time
}

func NewOpcoder(
	ctx context.Context,
	client EthClientOpcoder,
	tracker IssueTracker,
	maxFeeCap *big.Int,
	key *ecdsa.PrivateKey,
	issuePeriod time.Duration,
) (*Opcoder, error) {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	nonce, err := client.NonceAt(ctx, ethcrypto.PubkeyToAddress(key.PublicKey), big.NewInt(int64(rpc.PendingBlockNumber)))
	if err != nil {
		return nil, fmt.Errorf("getting nonce: %w", err)
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

	simulatorAddress, err := bind.WaitDeployed(ctx, client, simulatorDeploymentTx)
	if err != nil {
		return nil, fmt.Errorf("waiting for simulator contract to be mined: %w", err)
	}

	return &Opcoder{
		client:           client,
		tracker:          tracker,
		senderKey:        key,
		maxFeeCap:        maxFeeCap,
		issuePeriod:      issuePeriod,
		chainID:          chainID,
		maxTipCap:        maxTipCap,
		contractAddress:  simulatorAddress,
		contractInstance: simulatorInstance,
		nonce:            nonce,
	}, nil
}

func (o *Opcoder) GenerateAndIssueTx(ctx context.Context) (tx *types.Transaction, err error) {
	if o.issuePeriod > 0 && !o.lastIssue.IsZero() &&
		time.Since(o.lastIssue) < o.issuePeriod {
		timer := time.NewTimer(o.issuePeriod - time.Since(o.lastIssue))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}

	txOpts, err := o.newTxOpts(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating transaction opts: %w", err)
	}

	loadTypes := allLoadTypes()
	loadType := loadTypes[rand.IntN(len(loadTypes))] //nolint:gosec

	switch loadType {
	case randomWrite:
		const maxWriteSizeBytes = 5
		writeSize := big.NewInt(rand.Int64N(maxWriteSizeBytes)) //nolint:gosec
		tx, err = o.contractInstance.SimulateRandomWrite(txOpts, writeSize)
	case stateModification:
		const maxStateSizeBytes = 5
		stateSize := big.NewInt(rand.Int64N(maxStateSizeBytes)) //nolint:gosec
		tx, err = o.contractInstance.SimulateModification(txOpts, stateSize)
	case randomReads:
		const maxReadSizeBytes = 5
		numReads := big.NewInt(rand.Int64N(maxReadSizeBytes)) //nolint:gosec
		tx, err = o.contractInstance.SimulateReads(txOpts, numReads)
	case hashing:
		const maxRounds = 3
		rounds := big.NewInt(rand.Int64N(maxRounds)) //nolint:gosec
		tx, err = o.contractInstance.SimulateHashing(txOpts, rounds)
	case memory:
		const maxArraySize = 4
		arraySize := big.NewInt(rand.Int64N(maxArraySize)) //nolint:gosec
		tx, err = o.contractInstance.SimulateMemory(txOpts, arraySize)
	case callDepth:
		const maxDepth = 5
		depth := big.NewInt(rand.Int64N(maxDepth)) //nolint:gosec
		tx, err = o.contractInstance.SimulateCallDepth(txOpts, depth)
	default:
		return nil, fmt.Errorf("invalid load type: %s", loadType)
	}

	if err != nil {
		return nil, fmt.Errorf("calling simulator contract with load type %s: %w", loadType, err)
	}

	o.nonce++
	o.tracker.Issue(tx)
	o.lastIssue = time.Now()
	return tx, err
}

const (
	randomWrite       = "random write"
	stateModification = "state modification"
	randomReads       = "random reads"
	hashing           = "hashing"
	memory            = "memory"
	callDepth         = "call depth"
)

func allLoadTypes() []string {
	return []string{
		randomWrite,
		stateModification,
		randomReads,
		hashing,
		memory,
		callDepth,
	}
}

func (o *Opcoder) newTxOpts(ctx context.Context) (*bind.TransactOpts, error) {
	return newTxOpts(ctx, o.senderKey, o.chainID, o.maxFeeCap, o.maxTipCap, o.nonce)
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
