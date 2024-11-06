// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

var _ txs.Visitor = (*atomicTxVisitor)(nil)

// AtomicTx executes the atomic transaction [tx] and returns the resulting state
// modifications.
//
// This is only used to execute atomic transactions pre-AP5. After AP5 the
// execution was moved to be performed during standard transaction execution.
func AtomicTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	parentID ids.ID,
	stateVersions state.Versions,
	tx *txs.Tx,
) (state.Diff, set.Set[ids.ID], map[ids.ID]*atomic.Requests, error) {
	atomicExecutor := atomicTxVisitor{
		backend:       backend,
		feeCalculator: feeCalculator,
		parentID:      parentID,
		stateVersions: stateVersions,
		tx:            tx,
	}
	if err := tx.Unsigned.Visit(&atomicExecutor); err != nil {
		txID := tx.ID()
		return nil, nil, nil, fmt.Errorf("atomic tx %s failed execution: %w", txID, err)
	}
	return atomicExecutor.onAccept, atomicExecutor.inputs, atomicExecutor.atomicRequests, nil
}

type atomicTxVisitor struct {
	// inputs, to be filled before visitor methods are called
	backend       *Backend
	feeCalculator fee.Calculator
	parentID      ids.ID
	stateVersions state.Versions
	tx            *txs.Tx

	// outputs of visitor execution
	onAccept       state.Diff
	inputs         set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests
}

func (*atomicTxVisitor) AddValidatorTx(*txs.AddValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) CreateChainTx(*txs.CreateChainTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) BaseTx(*txs.BaseTx) error {
	return ErrWrongTxType
}

func (*atomicTxVisitor) ConvertSubnetTx(*txs.ConvertSubnetTx) error {
	return ErrWrongTxType
}

func (e *atomicTxVisitor) ImportTx(tx *txs.ImportTx) error {
	return e.atomicTx(tx)
}

func (e *atomicTxVisitor) ExportTx(tx *txs.ExportTx) error {
	return e.atomicTx(tx)
}

func (e *atomicTxVisitor) atomicTx(tx txs.UnsignedTx) error {
	onAccept, err := state.NewDiff(
		e.parentID,
		e.stateVersions,
	)
	if err != nil {
		return err
	}
	e.onAccept = onAccept

	executor := StandardTxExecutor{
		Backend:       e.backend,
		State:         e.onAccept,
		FeeCalculator: e.feeCalculator,
		Tx:            e.tx,
	}
	err = tx.Visit(&executor)
	e.inputs = executor.Inputs
	e.atomicRequests = executor.AtomicRequests
	return err
}
