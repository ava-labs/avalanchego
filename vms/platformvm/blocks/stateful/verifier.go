// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_ stateless.Visitor = &verifier{}

	errConflictingBatchTxs                   = errors.New("block contains conflicting transactions")
	errConflictingParentTxs                  = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
	errAdvanceTimeTxCannotBeIncluded         = errors.New("advance time tx cannot be included in block")
	errOptionBlockTimestampNotMatchingParent = errors.New("option block proposed timestamp not matching parent block one")
)

// verifier handles the logic for verifying a block.
type verifier struct {
	*backend
	man               *manager
	txExecutorBackend executor.Backend
}

func (v *verifier) VisitApricotProposalBlock(b *stateless.ApricotProposalBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	tx := b.BlockTxs()[0]
	parentID := b.Parent()
	parentState := v.OnAccept(parentID)
	txExecutor := executor.ProposalTxExecutor{
		Backend:     &v.txExecutorBackend,
		ParentState: parentState,
		Tx:          tx,
	}

	if err := tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := tx.ID()
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
	blkState.initiallyPreferCommit = txExecutor.PrefersCommit

	v.Mempool.RemoveProposalTx(b.Tx)
	v.blkIDToState[blkID] = blkState

	if parentBlockState, ok := v.blkIDToState[parentID]; ok {
		parentBlockState.children = append(parentBlockState.children, blkID)
	}
	return nil
}

func (v *verifier) VisitBlueberryProposalBlock(b *stateless.BlueberryProposalBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
	}

	tx := b.BlockTxs()[0]
	if _, ok := tx.Unsigned.(*txs.AdvanceTimeTx); ok {
		return errAdvanceTimeTxCannotBeIncluded
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState := v.OnAccept(parentID)

	// Having verifier block timestamp, we update staker set
	// before processing block transaction
	var (
		newlyCurrentStakers state.CurrentStakers
		newlyPendingStakers state.PendingStakers
		updatedSupply       uint64
	)
	nextChainTime := time.Unix(b.UnixTimestamp(), 0)
	currentStakers := parentState.CurrentStakers()
	pendingStakers := parentState.PendingStakers()
	currentSupply := parentState.GetCurrentSupply()
	newlyCurrentStakers,
		newlyPendingStakers,
		updatedSupply,
		err := executor.UpdateStakerSet(
		currentStakers,
		pendingStakers,
		currentSupply,
		&v.txExecutorBackend,
		nextChainTime,
	)
	if err != nil {
		return err
	}
	baseOptionsState := state.NewDiff(
		parentState,
		newlyCurrentStakers,
		newlyPendingStakers,
	)
	baseOptionsState.SetTimestamp(nextChainTime)
	baseOptionsState.SetCurrentSupply(updatedSupply)
	blkState.onBlueberryBaseOptionsState = baseOptionsState

	txExecutor := executor.ProposalTxExecutor{
		Backend:     &v.txExecutorBackend,
		ParentState: baseOptionsState,
		Tx:          tx,
	}

	if err := tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	onCommitState := txExecutor.OnCommit
	onCommitState.AddTx(b.Tx, status.Committed)
	blkState.onCommitState = onCommitState

	onAbortState := txExecutor.OnAbort
	onAbortState.AddTx(b.Tx, status.Aborted)
	blkState.onAbortState = onAbortState

	blkState.timestamp = time.Unix(b.UnixTimestamp(), 0)
	blkState.initiallyPreferCommit = txExecutor.PrefersCommit

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

	if err := v.verifyCommonBlock(b); err != nil {
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

	tx := b.BlockTxs()[0]
	atomicExecutor := executor.AtomicTxExecutor{
		Backend:     &v.txExecutorBackend,
		ParentState: parentState,
		Tx:          tx,
	}
	if err := tx.Unsigned.Visit(&atomicExecutor); err != nil {
		txID := tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(tx, status.Committed)

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
			return errConflictingParentTxs
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

func (v *verifier) VisitApricotStandardBlock(b *stateless.ApricotStandardBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	parentState := v.OnAccept(b.Parent())

	onAcceptState := state.NewDiff(
		parentState,
		parentState.CurrentStakers(),
		parentState.PendingStakers(),
	)

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
				return errConflictingParentTxs
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

func (v *verifier) VisitBlueberryStandardBlock(b *stateless.BlueberryStandardBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState := v.OnAccept(parentID)

	// We update staker set before processing block transactions
	nextChainTime := time.Unix(b.UnixTimestamp(), 0)
	currentStakers := parentState.CurrentStakers()
	pendingStakers := parentState.PendingStakers()
	currentSupply := parentState.GetCurrentSupply()
	newlyCurrentStakers,
		newlyPendingStakers,
		updatedSupply,
		err := executor.UpdateStakerSet(
		currentStakers,
		pendingStakers,
		currentSupply,
		&v.txExecutorBackend,
		nextChainTime,
	)
	if err != nil {
		return err
	}
	onAcceptState := state.NewDiff(
		parentState,
		newlyCurrentStakers,
		newlyPendingStakers,
	)
	onAcceptState.SetTimestamp(nextChainTime)
	onAcceptState.SetCurrentSupply(updatedSupply)

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
				return errConflictingParentTxs
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

	blkState.timestamp = nextChainTime
	blkState.onAcceptState = onAcceptState

	v.Mempool.RemoveDecisionTxs(b.Txs)
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

	parentID := b.Parent()
	if err := v.verifyCommonBlock(b); err != nil {
		return fmt.Errorf("couldn't verify common block of %s: %s", blkID, err)
	}

	blkVersion := b.Version()
	onAcceptState := v.blkIDToState[parentID].onCommitState
	if blkVersion == stateless.BlueberryVersion {
		if err := v.validateBlockTimestamp(
			b,
			v.blkIDToState[parentID].timestamp,
		); err != nil {
			return err
		}
		blkState.timestamp = time.Unix(b.UnixTimestamp(), 0)
	} else if blkVersion == stateless.ApricotVersion {
		blkState.timestamp = onAcceptState.GetTimestamp()
	}

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

	parentID := b.Parent()
	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	blkVersion := b.Version()
	onAcceptState := v.blkIDToState[parentID].onAbortState

	if blkVersion == stateless.BlueberryVersion {
		if err := v.validateBlockTimestamp(
			b,
			v.blkIDToState[parentID].timestamp,
		); err != nil {
			return err
		}
		blkState.timestamp = time.Unix(b.UnixTimestamp(), 0)
	} else if blkVersion == stateless.ApricotVersion {
		blkState.timestamp = onAcceptState.GetTimestamp()
	}

	blkState.timestamp = onAcceptState.GetTimestamp()
	blkState.onAcceptState = onAcceptState

	v.blkIDToState[blkID] = blkState

	parentState := v.blkIDToState[parentID]
	parentState.children = append(parentState.children, blkID)
	return nil
}

func (v *verifier) verifyCommonBlock(b stateless.Block) error {
	// retrieve parent block first
	parentID := b.Parent()
	parentBlk, err := v.man.GetBlock(parentID)
	if err != nil {
		return err
	}

	// verify block height
	if expectedHeight := parentBlk.Height() + 1; expectedHeight != b.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			b.Height(),
		)
	}

	// verify block version
	blkVersion := b.Version()
	expectedVersion := v.expectedChildVersion(parentBlk.Timestamp())
	if expectedVersion != blkVersion {
		return fmt.Errorf(
			"expected block to have version %d, but found %d",
			expectedVersion,
			blkVersion,
		)
	}

	return v.validateBlockTimestamp(b, parentBlk.Timestamp())
}

func (v *verifier) validateBlockTimestamp(blk stateless.Block, parentBlkTime time.Time) error {
	if blk.Version() == stateless.ApricotVersion {
		return nil
	}

	blkTime := time.Unix(blk.UnixTimestamp(), 0)

	switch blk.(type) {
	case *stateless.AbortBlock,
		*stateless.CommitBlock:
		if !blkTime.Equal(parentBlkTime) {
			return fmt.Errorf(
				"%w parent block timestamp (%s) option block timestamp (%s)",
				errOptionBlockTimestampNotMatchingParent,
				parentBlkTime,
				blkTime,
			)
		}
		return nil

	case *stateless.BlueberryStandardBlock,
		*stateless.BlueberryProposalBlock:
		parentState := v.OnAccept(blk.Parent())
		nextStakerChangeTime, err := parentState.GetNextStakerChangeTime()
		if err != nil {
			return fmt.Errorf("could not verify block timestamp: %w", err)
		}
		localTime := v.txExecutorBackend.Clk.Time()

		return executor.ValidateProposedChainTime(
			blkTime,
			parentBlkTime,
			nextStakerChangeTime,
			localTime,
			false, /*enforceStrictness*/
		)

	default:
		return fmt.Errorf(
			"cannot not validate block timestamp for block type %T",
			blk,
		)
	}
}
