// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

var _ platform.TxVisitor = (*atomicTxExecutor)(nil)

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
	tx *platform.Tx,
) (*state.Diff, set.Set[ids.ID], map[ids.ID]*atomic.Requests, error) {
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
	tx            *platform.Tx

	// outputs of visitor execution
	onAccept       *state.Diff
	inputs         set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests
}

func (*atomicTxExecutor) AddValidatorTx(*platform.AddValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddSubnetValidatorTx(*platform.AddSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddDelegatorTx(*platform.AddDelegatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) CreateChainTx(*platform.CreateChainTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) CreateSubnetTx(*platform.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AdvanceTimeTx(*platform.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RewardValidatorTx(*platform.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RemoveSubnetValidatorTx(*platform.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) TransformSubnetTx(*platform.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddPermissionlessValidatorTx(*platform.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddPermissionlessDelegatorTx(*platform.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) TransferSubnetOwnershipTx(*platform.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) BaseTx(*platform.BaseTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) ConvertSubnetToL1Tx(*platform.ConvertSubnetToL1Tx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RegisterL1ValidatorTx(*platform.RegisterL1ValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) SetL1ValidatorWeightTx(*platform.SetL1ValidatorWeightTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) IncreaseL1ValidatorBalanceTx(*platform.IncreaseL1ValidatorBalanceTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) DisableL1ValidatorTx(*platform.DisableL1ValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) AddAutoRenewedValidatorTx(*platform.AddAutoRenewedValidatorTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) SetAutoRenewedValidatorConfigTx(*platform.SetAutoRenewedValidatorConfigTx) error {
	return ErrWrongTxType
}

func (*atomicTxExecutor) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	return ErrWrongTxType
}

func (e *atomicTxExecutor) ImportTx(*platform.ImportTx) error {
	return e.atomicTx()
}

func (e *atomicTxExecutor) ExportTx(*platform.ExportTx) error {
	return e.atomicTx()
}

func (e *atomicTxExecutor) atomicTx() error {
	onAccept, err := state.NewDiff(
		e.parentID,
		e.stateVersions,
		state.StakerAdditionAfterDeletionForbidden,
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
