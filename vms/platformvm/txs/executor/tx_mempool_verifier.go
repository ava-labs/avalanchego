// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = &MempoolTxVerifier{}

type MempoolTxVerifier struct {
	*Backend
	ParentID      ids.ID
	StateVersions state.Versions
	Tx            *txs.Tx
}

func (*MempoolTxVerifier) AdvanceTimeTx(*txs.AdvanceTimeTx) error         { return errWrongTxType }
func (*MempoolTxVerifier) RewardValidatorTx(*txs.RewardValidatorTx) error { return errWrongTxType }

func (v *MempoolTxVerifier) AddValidatorTx(tx *txs.AddValidatorTx) error {
	return v.proposalTx(tx)
}

func (v *MempoolTxVerifier) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	return v.proposalTx(tx)
}

func (v *MempoolTxVerifier) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	return v.proposalTx(tx)
}

func (v *MempoolTxVerifier) CreateChainTx(tx *txs.CreateChainTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) ImportTx(tx *txs.ImportTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) ExportTx(tx *txs.ExportTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	return v.standardTx(tx)
}

func (v *MempoolTxVerifier) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	return v.standardTx(tx)
}

// TODO: simplify this function after Banff is activated.
func (v *MempoolTxVerifier) proposalTx(tx txs.StakerTx) error {
	startTime := tx.StartTime()
	maxLocalStartTime := v.Clk.Time().Add(MaxFutureStartTime)
	if startTime.After(maxLocalStartTime) {
		return errFutureStakeTime
	}

	onCommitState, err := state.NewDiff(v.ParentID, v.StateVersions)
	if err != nil {
		return err
	}

	// Make sure that the Banff fork check will pass.
	currentChainTime := onCommitState.GetTimestamp()
	if v.Backend.Config.IsBanffActivated(currentChainTime) {
		return v.standardTx(tx)
	}

	onAbortState, err := state.NewDiff(v.ParentID, v.StateVersions)
	if err != nil {
		return err
	}

	executor := ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       v.Backend,
		Tx:            v.Tx,
	}
	err = tx.Visit(&executor)
	// We ignore [errFutureStakeTime] here because the time will be advanced
	// when this transaction is issued.
	if errors.Is(err, errFutureStakeTime) {
		return nil
	}
	return err
}

func (v *MempoolTxVerifier) standardTx(tx txs.UnsignedTx) error {
	state, err := state.NewDiff(
		v.ParentID,
		v.StateVersions,
	)
	if err != nil {
		return err
	}

	executor := StandardTxExecutor{
		Backend: v.Backend,
		State:   state,
		Tx:      v.Tx,
	}
	err = tx.Visit(&executor)
	// We ignore [errFutureStakeTime] here because the time will be advanced
	// when this transaction is issued.
	if errors.Is(err, errFutureStakeTime) {
		return nil
	}
	return err
}
