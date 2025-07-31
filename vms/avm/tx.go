// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/executor"
)

var (
	_ snowstorm.Tx = (*Tx)(nil)

	errTxNotProcessing  = errors.New("transaction is not processing")
	errUnexpectedReject = errors.New("attempting to reject transaction")
)

type Tx struct {
	vm *VM
	tx *txs.Tx
}

func (tx *Tx) ID() ids.ID {
	return tx.tx.ID()
}

func (tx *Tx) Accept(context.Context) error {
	if s := tx.Status(); s != choices.Processing {
		return fmt.Errorf("%w: %s", errTxNotProcessing, s)
	}

	tx.vm.onAccept(tx.tx)

	executor := &executor.Executor{
		Codec: tx.vm.txBackend.Codec,
		State: tx.vm.state,
		Tx:    tx.tx,
	}
	err := tx.tx.Unsigned.Visit(executor)
	if err != nil {
		return fmt.Errorf("error staging accepted state changes: %w", err)
	}

	tx.vm.state.AddTx(tx.tx)

	commitBatch, err := tx.vm.state.CommitBatch()
	if err != nil {
		txID := tx.tx.ID()
		return fmt.Errorf("couldn't create commitBatch while processing tx %s: %w", txID, err)
	}

	defer tx.vm.state.Abort()
	err = tx.vm.ctx.SharedMemory.Apply(
		executor.AtomicRequests,
		commitBatch,
	)
	if err != nil {
		txID := tx.tx.ID()
		return fmt.Errorf("error committing accepted state changes while processing tx %s: %w", txID, err)
	}

	return tx.vm.metrics.MarkTxAccepted(tx.tx)
}

func (*Tx) Reject(context.Context) error {
	return errUnexpectedReject
}

func (tx *Tx) Status() choices.Status {
	txID := tx.tx.ID()
	_, err := tx.vm.state.GetTx(txID)
	switch err {
	case nil:
		return choices.Accepted
	case database.ErrNotFound:
		return choices.Processing
	default:
		tx.vm.ctx.Log.Error("failed looking up tx status",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)
		return choices.Processing
	}
}

func (tx *Tx) MissingDependencies() (set.Set[ids.ID], error) {
	txIDs := set.Set[ids.ID]{}
	for _, in := range tx.tx.Unsigned.InputUTXOs() {
		if in.Symbolic() {
			continue
		}
		txID, _ := in.InputSource()

		_, err := tx.vm.state.GetTx(txID)
		switch err {
		case nil:
			// Tx was already accepted
		case database.ErrNotFound:
			txIDs.Add(txID)
		default:
			return nil, err
		}
	}
	return txIDs, nil
}

func (tx *Tx) Bytes() []byte {
	return tx.tx.Bytes()
}

func (tx *Tx) Verify(context.Context) error {
	if s := tx.Status(); s != choices.Processing {
		return fmt.Errorf("%w: %s", errTxNotProcessing, s)
	}
	return tx.tx.Unsigned.Visit(&executor.SemanticVerifier{
		Backend: tx.vm.txBackend,
		State:   tx.vm.state,
		Tx:      tx.tx,
	})
}
