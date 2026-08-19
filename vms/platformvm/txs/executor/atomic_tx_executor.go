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
	wrongTxType

	// inputs, to be filled before visitor methods are called
	backend       *Backend
	feeCalculator fee.Calculator
	parentID      ids.ID
	stateVersions state.Versions
	tx            *txs.Tx

	// outputs of visitor execution
	onAccept       *state.Diff
	inputs         set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests
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
