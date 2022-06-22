// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = &atomicTxExecutor{}

// atomicTxExecutor is used to execute atomic transactions pre-AP5. After AP5
// the execution was moved to be performed inside of the standardTxExecutor.
type atomicTxExecutor struct {
	// inputs
	vm          *VM
	parentState state.Chain
	tx          *txs.Tx

	// outputs
	onAccept       state.Diff
	inputs         ids.Set
	atomicRequests map[ids.ID]*atomic.Requests
}

func (*atomicTxExecutor) AddValidatorTx(*txs.AddValidatorTx) error             { return errWrongTxType }
func (*atomicTxExecutor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error { return errWrongTxType }
func (*atomicTxExecutor) AddDelegatorTx(*txs.AddDelegatorTx) error             { return errWrongTxType }
func (*atomicTxExecutor) CreateChainTx(*txs.CreateChainTx) error               { return errWrongTxType }
func (*atomicTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error             { return errWrongTxType }
func (*atomicTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error               { return errWrongTxType }
func (*atomicTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error       { return errWrongTxType }

func (e *atomicTxExecutor) ImportTx(tx *txs.ImportTx) error {
	return e.atomicTx(tx)
}

func (e *atomicTxExecutor) ExportTx(tx *txs.ExportTx) error {
	return e.atomicTx(tx)
}

func (e *atomicTxExecutor) atomicTx(tx txs.UnsignedTx) error {
	e.onAccept = state.NewDiff(
		e.parentState,
		e.parentState.CurrentStakers(),
		e.parentState.PendingStakers(),
	)
	executor := standardTxExecutor{
		vm:    e.vm,
		state: e.onAccept,
		tx:    e.tx,
	}
	err := tx.Visit(&executor)
	e.inputs = executor.inputs
	e.atomicRequests = executor.atomicRequests
	return err
}
