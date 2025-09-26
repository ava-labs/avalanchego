// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

var _ txs.Visitor = (*atomicTxExecutor)(nil)

// AtomicTx executes the atomic transaction [tx] and returns the resulting state
// modifications.
//
// This is only used to execute atomic transactions pre-AP5. After AP5 the
// execution was moved to [StandardTx].
func AtomicTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	parentID ids.ID,
	stateVersions state.Versions,
	tx *txs.Tx,
) (state.Diff, set.Set[ids.ID], map[ids.ID]*atomic.Requests, error) {
	atomicExecutor := atomicTxExecutor{
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

type atomicTxExecutor struct {
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

func (*atomicTxExecutor) AddValidatorTx(*txs.AddValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) CreateChainTx(*txs.CreateChainTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RewardContinuousValidatorTx(tx *txs.RewardContinuousValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) BaseTx(*txs.BaseTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) ConvertSubnetToL1Tx(*txs.ConvertSubnetToL1Tx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RegisterL1ValidatorTx(*txs.RegisterL1ValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) SetL1ValidatorWeightTx(*txs.SetL1ValidatorWeightTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) IncreaseL1ValidatorBalanceTx(*txs.IncreaseL1ValidatorBalanceTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) DisableL1ValidatorTx(*txs.DisableL1ValidatorTx) error {
	return ErrWrongTxType
}

func (e *atomicTxExecutor) AddContinuousValidatorTx(tx *txs.AddContinuousValidatorTx) error {
	return ErrWrongTxType
}

func (e *atomicTxExecutor) StopContinuousValidatorTx(tx *txs.StopContinuousValidatorTx) error {
	return ErrWrongTxType
}

func (e *atomicTxExecutor) ImportTx(*txs.ImportTx) error {
	return e.atomicTx()
}

func (e *atomicTxExecutor) ExportTx(*txs.ExportTx) error {
	return e.atomicTx()
}

func (e *atomicTxExecutor) atomicTx() error {
	onAccept, err := state.NewDiff(
		e.parentID,
		e.stateVersions,
	)
	if err != nil {
		return err
	}

	e.onAccept = onAccept
	e.inputs, e.atomicRequests, _, err = StandardTx(
		e.backend,
		e.feeCalculator,
		e.tx,
		onAccept,
	)
	return err
}
