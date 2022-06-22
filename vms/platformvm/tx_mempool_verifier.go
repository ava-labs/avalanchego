// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = &mempoolTxVerifier{}

type mempoolTxVerifier struct {
	vm          *VM
	parentState state.Chain
	tx          *txs.Tx
}

func (*mempoolTxVerifier) AdvanceTimeTx(*txs.AdvanceTimeTx) error         { return errWrongTxType }
func (*mempoolTxVerifier) RewardValidatorTx(*txs.RewardValidatorTx) error { return errWrongTxType }

func (v *mempoolTxVerifier) AddValidatorTx(tx *txs.AddValidatorTx) error {
	return v.proposalTx(tx)
}

func (v *mempoolTxVerifier) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	return v.proposalTx(tx)
}

func (v *mempoolTxVerifier) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	return v.proposalTx(tx)
}

func (v *mempoolTxVerifier) CreateChainTx(tx *txs.CreateChainTx) error {
	return v.standardTx(tx)
}

func (v *mempoolTxVerifier) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	return v.standardTx(tx)
}

func (v *mempoolTxVerifier) ImportTx(tx *txs.ImportTx) error {
	return v.standardTx(tx)
}

func (v *mempoolTxVerifier) ExportTx(tx *txs.ExportTx) error {
	return v.standardTx(tx)
}

func (v *mempoolTxVerifier) proposalTx(tx txs.StakerTx) error {
	startTime := tx.StartTime()
	maxLocalStartTime := v.vm.clock.Time().Add(maxFutureStartTime)
	if startTime.After(maxLocalStartTime) {
		return errFutureStakeTime
	}

	executor := proposalTxExecutor{
		vm:          v.vm,
		parentState: v.parentState,
		tx:          v.tx,
	}
	err := tx.Visit(&executor)
	// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
	// issued before this transaction is issued.
	if errors.Is(err, errFutureStakeTime) {
		return nil
	}
	return err
}

func (v *mempoolTxVerifier) standardTx(tx txs.UnsignedTx) error {
	executor := standardTxExecutor{
		vm: v.vm,
		state: state.NewDiff(
			v.parentState,
			v.parentState.CurrentStakers(),
			v.parentState.PendingStakers(),
		),
		tx: v.tx,
	}
	return tx.Visit(&executor)
}
