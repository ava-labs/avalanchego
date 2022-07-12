// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_                       stateless.Visitor = &verifier{}
	errConflictingBatchTxs                    = errors.New("block contains conflicting transactions")
	ErrConflictingParentTxs                   = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
)

type verifier struct {
	backend
	txExecutorBackend executor.Backend
}

func (v *verifier) VisitProposalBlock(b *stateless.ProposalBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState := v.OnAccept(parentID) // TODO is this right?

	txExecutor := executor.ProposalTxExecutor{
		Backend:     &v.txExecutorBackend,
		ParentState: parentState,
		Tx:          b.Tx,
	}
	if err := b.Tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	onCommitState := txExecutor.OnCommit
	onCommitState.AddTx(b.Tx, status.Committed)
	blkState.onCommitState = onCommitState

	onAbortState := txExecutor.OnAbort
	onAbortState.AddTx(b.Tx, status.Aborted)
	blkState.onAbortState = onAbortState

	blkState.timestamp = parentState.GetTimestamp()
	blkState.inititallyPreferCommit = txExecutor.PrefersCommit

	v.Mempool.RemoveProposalTx(b.Tx)
	v.blkIDToState[blkID] = blkState

	if parentBlockState, ok := v.blkIDToState[parentID]; ok {
		parentBlockState.children = append(parentBlockState.children, blkID)
	}
	return nil
}

func (v *verifier) VisitAtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	parentState := v.OnAccept(b.Parent())

	cfg := v.txExecutorBackend.Cfg
	currentTimestamp := parentState.GetTimestamp()
	enbledAP5 := !currentTimestamp.Before(cfg.ApricotPhase5Time)

	if enbledAP5 {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			cfg.ApricotPhase5Time.Unix(),
		)
	}

	atomicExecutor := executor.AtomicTxExecutor{
		Backend:     &v.txExecutorBackend,
		ParentState: parentState,
		Tx:          b.Tx,
	}
	if err := b.Tx.Unsigned.Visit(&atomicExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(b.Tx, status.Committed)

	blkState.onAcceptState = atomicExecutor.OnAccept
	blkState.inputs = atomicExecutor.Inputs
	blkState.atomicRequests = atomicExecutor.AtomicRequests
	blkState.timestamp = atomicExecutor.OnAccept.GetTimestamp()

	// Check for conflicts in atomic inputs.
	var nextBlock stateless.Block = b
	for {
		parentID := nextBlock.Parent()
		parentState := v.blkIDToState[parentID]
		if parentState == nil {
			// The parent state isn't pinned in memory.
			// This means the parent must be accepted already.
			break
		}
		if parentState.inputs.Overlaps(atomicExecutor.Inputs) {
			return ErrConflictingParentTxs
		}
		parent, _, err := v.state.GetStatelessBlock(parentID)
		if err != nil {
			// The parent isn't in memory, so it should be on disk,
			// but it isn't.
			return err
		}
		nextBlock = parent
	}

	v.Mempool.RemoveDecisionTxs([]*txs.Tx{b.Tx})
	parentID := b.Parent()
	if parentBlockState, ok := v.blkIDToState[parentID]; ok {
		parentBlockState.children = append(parentBlockState.children, blkID)
	}
	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) VisitStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	parentState := v.OnAccept(b.Parent())

	onAcceptState := state.NewDiff(
		parentState,
		parentState.CurrentStakers(),
		parentState.PendingStakers(),
	)

	// TODO do we still need to do something similar to the below?
	// clear inputs so that multiple [Verify] calls can be made
	// b.Inputs.Clear()
	// b.atomicRequests = make(map[ids.ID]*atomic.Requests)

	funcs := make([]func(), 0, len(b.Txs))
	for _, tx := range b.Txs {
		txExecutor := executor.StandardTxExecutor{
			Backend: &v.txExecutorBackend,
			State:   onAcceptState,
			Tx:      tx,
		}
		if err := tx.Unsigned.Visit(&txExecutor); err != nil {
			txID := tx.ID()
			v.MarkDropped(txID, err.Error()) // cache tx as dropped
			return err
		}
		// ensure it doesn't overlap with current input batch
		if blkState.inputs.Overlaps(txExecutor.Inputs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		blkState.inputs.Union(txExecutor.Inputs)

		onAcceptState.AddTx(tx, status.Committed)
		if txExecutor.OnAccept != nil {
			funcs = append(funcs, txExecutor.OnAccept)
		}

		for chainID, txRequests := range txExecutor.AtomicRequests {
			// Add/merge in the atomic requests represented by [tx]
			chainRequests, exists := blkState.atomicRequests[chainID]
			if !exists {
				blkState.atomicRequests[chainID] = txRequests
				continue
			}

			chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
			chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
		}
	}

	// Check for conflicts in ancestors.
	if blkState.inputs.Len() > 0 {
		var nextBlock stateless.Block = b
		for {
			parentID := nextBlock.Parent()
			parentState := v.blkIDToState[parentID]
			if parentState == nil {
				// The parent state isn't pinned in memory.
				// This means the parent must be accepted already.
				break
			}
			if parentState.inputs.Overlaps(blkState.inputs) {
				return ErrConflictingParentTxs
			}
			var parent stateless.Block
			if parentState, ok := v.blkIDToState[parentID]; ok {
				// The parent is in memory.
				parent = parentState.statelessBlock
			} else {
				var err error
				parent, _, err = v.state.GetStatelessBlock(parentID)
				if err != nil {
					return err
				}
			}
			nextBlock = parent
		}
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		blkState.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		blkState.onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	blkState.timestamp = onAcceptState.GetTimestamp()
	blkState.onAcceptState = onAcceptState
	v.Mempool.RemoveDecisionTxs(b.Txs)
	parentID := b.Parent()
	if parentBlockState, ok := v.blkIDToState[parentID]; ok {
		parentBlockState.children = append(parentBlockState.children, blkID)
	}

	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) VisitCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return fmt.Errorf("couldn't verify common block of %s: %s", blkID, err)
	}

	parentID := b.Parent()
	onAcceptState := v.blkIDToState[parentID].onCommitState
	blkState.timestamp = onAcceptState.GetTimestamp()
	blkState.onAcceptState = onAcceptState

	v.blkIDToState[blkID] = blkState

	parentState := v.blkIDToState[parentID]
	parentState.children = append(parentState.children, blkID)

	return nil
}

func (v *verifier) VisitAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
	}

	if err := v.verifyCommonBlock(b.CommonBlock); err != nil {
		return err
	}

	parentID := b.Parent()
	onAcceptState := v.blkIDToState[parentID].onAbortState
	blkState.timestamp = onAcceptState.GetTimestamp()
	blkState.onAcceptState = onAcceptState

	v.blkIDToState[blkID] = blkState

	parentState := v.blkIDToState[parentID]
	parentState.children = append(parentState.children, blkID)
	return nil
}

// Assumes [b] isn't nil
func (v *verifier) verifyCommonBlock(b stateless.CommonBlock) error {
	var (
		parentID           = b.Parent()
		parentStatelessBlk stateless.Block
	)
	// Check if the parent is in memory.
	if parent, ok := v.blkIDToState[parentID]; ok {
		parentStatelessBlk = parent.statelessBlock
	} else {
		// The parent isn't in memory.
		var err error
		parentStatelessBlk, _, err = v.state.GetStatelessBlock(parentID)
		if err != nil {
			return err
		}
	}
	if expectedHeight := parentStatelessBlk.Height() + 1; expectedHeight != b.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			b.Height(),
		)
	}
	return nil
}
