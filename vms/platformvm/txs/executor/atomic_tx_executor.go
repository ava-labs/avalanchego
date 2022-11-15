// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = (*AtomicTxExecutor)(nil)

// atomicTxExecutor is used to execute atomic transactions pre-AP5. After AP5
// the execution was moved to be performed inside of the standardTxExecutor.
type AtomicTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	ParentID      ids.ID
	StateVersions state.Versions
	Tx            *txs.Tx

	// outputs of visitor execution
	OnAccept       state.Diff
	Inputs         ids.Set
	AtomicRequests map[ids.ID]*atomic.Requests
}

func (*AtomicTxExecutor) AddValidatorTx(*txs.AddValidatorTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) CreateChainTx(*txs.CreateChainTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return errWrongTxType
}

func (*AtomicTxExecutor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return errWrongTxType
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
		Backend: e.Backend,
		State:   e.OnAccept,
		Tx:      e.Tx,
	}
	err = tx.Visit(&executor)
	e.Inputs = executor.Inputs
	e.AtomicRequests = executor.AtomicRequests
	return err
}
