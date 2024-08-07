// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

var _ txs.Visitor = (*AtomicTxExecutor)(nil)

// atomicTxExecutor is used to execute atomic transactions pre-AP5. After AP5
// the execution was moved to be performed inside of the standardTxExecutor.
type AtomicTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	FeeCalculator fee.Calculator
	ParentID      ids.ID
	StateVersions state.Versions
	Tx            *txs.Tx

	// outputs of visitor execution
	OnAccept       state.Diff
	Inputs         set.Set[ids.ID]
	AtomicRequests map[ids.ID]*atomic.Requests
}

func (*AtomicTxExecutor) AddValidatorTx(*txs.AddValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) CreateChainTx(*txs.CreateChainTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) BaseTx(*txs.BaseTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) ConvertSubnetTx(*txs.ConvertSubnetTx) error {
	return ErrWrongTxType
}

func (e *AtomicTxExecutor) ImportTx(tx *txs.ImportTx) error {
	return e.atomicTx(tx)
}

func (e *AtomicTxExecutor) ExportTx(tx *txs.ExportTx) error {
	return e.atomicTx(tx)
}

func (e *AtomicTxExecutor) atomicTx(tx txs.UnsignedTx) error {
	onAccept, err := state.NewDiff(
		e.ParentID,
		e.StateVersions,
	)
	if err != nil {
		return err
	}
	e.OnAccept = onAccept

	executor := StandardTxExecutor{
		Backend:       e.Backend,
		State:         e.OnAccept,
		FeeCalculator: e.FeeCalculator,
		Tx:            e.Tx,
	}
	err = tx.Visit(&executor)
	e.Inputs = executor.Inputs
	e.AtomicRequests = executor.AtomicRequests
	return err
}
