// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ AtomicExecutor = &atomicExecutor{}

type AtomicExecutor interface {
	ExecuteAtomicTx(
		stx *signed.Tx,
		vs state.Versioned,
	) (func() error, error)

	// Accept this transaction with the additionally provided state transitions.
	// Accept this transaction and spend imported inputs
	// We spend imported UTXOs here rather than in semanticVerify because
	// we don't want to remove an imported UTXO in semanticVerify
	// only to have the transaction not be Accepted. This would be inconsistent.
	// Recall that imported UTXOs are not kept in a versionDB.
	AtomicAccept(stx *signed.Tx, ctx *snow.Context, batch database.Batch) error

	semanticVerifyAtomic(
		stx *signed.Tx,
		parentState state.Mutable,
	) error
}

type atomicExecutor struct {
	*decisionExecutor
}

func (ae *atomicExecutor) ExecuteAtomicTx(
	stx *signed.Tx,
	vs state.Versioned,
) (func() error, error) {
	var (
		txID  = stx.ID()
		creds = stx.Creds
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.ExportTx:
		return ae.executeExport(vs, utx, txID, creds)
	case *unsigned.ImportTx:
		return ae.executeImport(vs, utx, txID, creds)
	default:
		return nil, fmt.Errorf("expected atomic tx but got %T", utx)
	}
}

func (ae *atomicExecutor) AtomicAccept(stx *signed.Tx, ctx *snow.Context, batch database.Batch) error {
	chainID, requests, err := ae.AtomicOperations(stx)
	if err != nil {
		return err
	}
	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{chainID: requests}, batch)
}

func (ae *atomicExecutor) semanticVerifyAtomic(
	stx *signed.Tx,
	parentState state.Mutable,
) error {
	switch utx := stx.Unsigned.(type) {
	case *unsigned.ExportTx,
		*unsigned.ImportTx:
		vs := state.NewVersioned(
			parentState,
			parentState.CurrentStakerChainState(),
			parentState.PendingStakerChainState(),
		)
		_, err := ae.ExecuteAtomicTx(stx, vs)
		return err

	default:
		return fmt.Errorf("tx type %T could not be semantically verified", utx)
	}
}
