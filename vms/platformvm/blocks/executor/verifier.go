// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/forks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_ blocks.Visitor = &verifier{}

	errConflictingBatchTxs                   = errors.New("block contains conflicting transactions")
	errConflictingParentTxs                  = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
	errAdvanceTimeTxCannotBeIncluded         = errors.New("advance time tx cannot be included in BlueberryProposalBlock")
	errOptionBlockTimestampNotMatchingParent = errors.New("option block proposed timestamp not matching parent block one")
)

// verifier handles the logic for verifying a block.
type verifier struct {
	*backend
	txExecutorBackend *executor.Backend
	clk               *mockable.Clock
}

func (v *verifier) BlueberryAbortBlock(b *blocks.BlueberryAbortBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.blueberryOption(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkState := &blockState{
		statelessBlock: b,
		timestamp:      b.Timestamp(),
		onAcceptState:  parentState.onAbortState,
	}
	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) BlueberryCommitBlock(b *blocks.BlueberryCommitBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.blueberryOption(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  parentState.onCommitState,
		timestamp:      b.Timestamp(),
	}
	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) BlueberryProposalBlock(b *blocks.BlueberryProposalBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if _, ok := b.Tx.Unsigned.(*txs.AdvanceTimeTx); ok {
		return errAdvanceTimeTxCannotBeIncluded
	}

	if err := v.blueberryNonOption(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	onCommitState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}
	onAbortState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	// Apply the changes, if any, from advancing the chain time.
	nextChainTime := b.Timestamp()
	updated, err := executor.UpdateStakerSet(parentState, nextChainTime, v.txExecutorBackend.Rewards)
	if err != nil {
		return err
	}
	for _, state := range []state.Diff{onCommitState, onAbortState} {
		state.SetTimestamp(nextChainTime)
		state.SetCurrentSupply(updated.Supply)

		for _, currentValidatorToAdd := range updated.CurrentValidatorsToAdd {
			state.PutCurrentValidator(currentValidatorToAdd)
		}
		for _, pendingValidatorToRemove := range updated.PendingValidatorsToRemove {
			state.DeletePendingValidator(pendingValidatorToRemove)
		}
		for _, currentDelegatorToAdd := range updated.CurrentDelegatorsToAdd {
			state.PutCurrentDelegator(currentDelegatorToAdd)
		}
		for _, pendingDelegatorToRemove := range updated.PendingDelegatorsToRemove {
			state.DeletePendingDelegator(pendingDelegatorToRemove)
		}
		for _, currentValidatorToRemove := range updated.CurrentValidatorsToRemove {
			state.DeleteCurrentValidator(currentValidatorToRemove)
		}
	}

	// Check the transaction's validity.
	txExecutor := executor.ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       v.txExecutorBackend,
		Tx:            b.Tx,
	}

	if err := b.Tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	onCommitState.AddTx(b.Tx, status.Committed)
	onAbortState.AddTx(b.Tx, status.Aborted)

	blkState := &blockState{
		proposalBlockState: proposalBlockState{
			initiallyPreferCommit: txExecutor.PrefersCommit,
			onCommitState:         onCommitState,
			onAbortState:          onAbortState,
		},
		statelessBlock: b,
		timestamp:      b.Timestamp(),
	}

	v.blkIDToState[blkID] = blkState
	v.Mempool.RemoveProposalTx(b.Tx)
	return nil
}

func (v *verifier) BlueberryStandardBlock(b *blocks.BlueberryStandardBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.blueberryNonOption(b); err != nil {
		return err
	}

	blkState := &blockState{
		statelessBlock: b,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}
	nextChainTime := b.Timestamp()

	// Having verifier block timestamp, we update staker set
	// before processing block transaction
	updated, err := executor.UpdateStakerSet(parentState, nextChainTime, v.txExecutorBackend.Rewards)
	if err != nil {
		return err
	}

	onAcceptState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}
	onAcceptState.SetTimestamp(nextChainTime)
	onAcceptState.SetCurrentSupply(updated.Supply)

	for _, currentValidatorToAdd := range updated.CurrentValidatorsToAdd {
		onAcceptState.PutCurrentValidator(currentValidatorToAdd)
	}
	for _, pendingValidatorToRemove := range updated.PendingValidatorsToRemove {
		onAcceptState.DeletePendingValidator(pendingValidatorToRemove)
	}
	for _, currentDelegatorToAdd := range updated.CurrentDelegatorsToAdd {
		onAcceptState.PutCurrentDelegator(currentDelegatorToAdd)
	}
	for _, pendingDelegatorToRemove := range updated.PendingDelegatorsToRemove {
		onAcceptState.DeletePendingDelegator(pendingDelegatorToRemove)
	}
	for _, currentValidatorToRemove := range updated.CurrentValidatorsToRemove {
		onAcceptState.DeleteCurrentValidator(currentValidatorToRemove)
	}

	// Finally we process block transaction
	funcs := make([]func(), 0, len(b.Transactions))
	for _, tx := range b.Transactions {
		txExecutor := executor.StandardTxExecutor{
			Backend: v.txExecutorBackend,
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
		var nextBlock blocks.Block = b
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
			var parent blocks.Block
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

	v.blkIDToState[blkID] = blkState
	v.Mempool.RemoveDecisionTxs(b.Transactions)
	return nil
}

func (v *verifier) ApricotAbortBlock(b *blocks.ApricotAbortBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.commonBlock(b, forks.Apricot); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}
	onAcceptState := parentState.onAbortState

	blkState := &blockState{
		statelessBlock: b,
		timestamp:      onAcceptState.GetTimestamp(),
		onAcceptState:  onAcceptState,
	}
	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) ApricotCommitBlock(b *blocks.ApricotCommitBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.commonBlock(b, forks.Apricot); err != nil {
		return fmt.Errorf("couldn't verify common block of %s: %w", blkID, err)
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}
	onAcceptState := parentState.onCommitState

	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  onAcceptState,
		timestamp:      onAcceptState.GetTimestamp(),
	}
	v.blkIDToState[blkID] = blkState
	return nil
}

func (v *verifier) ApricotProposalBlock(b *blocks.ApricotProposalBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.commonBlock(b, forks.Apricot); err != nil {
		return err
	}

	parentID := b.Parent()
	onCommitState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}
	onAbortState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	txExecutor := &executor.ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       v.txExecutorBackend,
		Tx:            b.Tx,
	}
	if err := b.Tx.Unsigned.Visit(txExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	onCommitState.AddTx(b.Tx, status.Committed)
	onAbortState.AddTx(b.Tx, status.Aborted)

	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,
		proposalBlockState: proposalBlockState{
			onCommitState:         onCommitState,
			onAbortState:          onAbortState,
			initiallyPreferCommit: txExecutor.PrefersCommit,
		},
		// It is safe to use [b.onAbortState] here because the timestamp will
		// never be modified by an Apricot Abort block.
		timestamp: onAbortState.GetTimestamp(),
	}

	v.Mempool.RemoveProposalTx(b.Tx)
	return nil
}

func (v *verifier) ApricotStandardBlock(b *blocks.ApricotStandardBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	if err := v.commonBlock(b, forks.Apricot); err != nil {
		return err
	}

	parentID := b.Parent()
	onAcceptState, err := state.NewDiff(parentID, v)
	if err != nil {
		return err
	}

	funcs := make([]func(), 0, len(b.Transactions))
	for _, tx := range b.Transactions {
		txExecutor := &executor.StandardTxExecutor{
			Backend: v.txExecutorBackend,
			State:   onAcceptState,
			Tx:      tx,
		}
		if err := tx.Unsigned.Visit(txExecutor); err != nil {
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

	if err := v.verifyUniqueInputs(b, blkState.inputs); err != nil {
		return err
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

	v.blkIDToState[blkID] = blkState
	v.Mempool.RemoveDecisionTxs(b.Transactions)
	return nil
}

func (v *verifier) commonBlock(b blocks.Block, expectedFork forks.Fork) error {
	parentID := b.Parent()
	parent, err := v.GetBlock(parentID)
	if err != nil {
		return err
	}

	// check height
	if expectedHeight := parent.Height() + 1; expectedHeight != b.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			b.Height(),
		)
	}

	// check fork
	if currentFork := v.GetFork(parentID); currentFork != expectedFork {
		return fmt.Errorf("expected fork %d but got %d", expectedFork, currentFork)
	}
	return nil
}

func (v *verifier) blueberryOption(b blocks.BlueberryBlock) error {
	if err := v.commonBlock(b, forks.Blueberry); err != nil {
		return err
	}

	parentID := b.Parent()
	parentBlk, err := v.GetBlock(parentID)
	if err != nil {
		return err
	}
	parentBlkTime := v.getTimestamp(parentBlk)
	blkTime := b.Timestamp()

	if !blkTime.Equal(parentBlkTime) {
		return fmt.Errorf(
			"%w parent block timestamp (%s) option block timestamp (%s)",
			errOptionBlockTimestampNotMatchingParent,
			parentBlkTime,
			blkTime,
		)
	}
	return nil
}

func (v *verifier) blueberryNonOption(b blocks.BlueberryBlock) error {
	if err := v.commonBlock(b, forks.Blueberry); err != nil {
		return err
	}

	parentID := b.Parent()
	parentBlk, err := v.GetBlock(parentID)
	if err != nil {
		return err
	}
	parentBlkTime := v.getTimestamp(parentBlk)
	blkTime := b.Timestamp()

	parentState, ok := v.GetState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}
	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(parentState)
	if err != nil {
		return fmt.Errorf("could not verify block timestamp: %w", err)
	}
	localTime := v.clk.Time()

	return executor.ValidateProposedChainTime(
		blkTime,
		parentBlkTime,
		nextStakerChangeTime,
		localTime,
		false, /*enforceStrictness*/
	)
}

func (v *verifier) ApricotAtomicBlock(b *blocks.ApricotAtomicBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.commonBlock(b, forks.Apricot); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	cfg := v.txExecutorBackend.Config
	currentTimestamp := parentState.GetTimestamp()
	if enbledAP5 := !currentTimestamp.Before(cfg.ApricotPhase5Time); enbledAP5 {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			cfg.ApricotPhase5Time.Unix(),
		)
	}

	atomicExecutor := &executor.AtomicTxExecutor{
		Backend:       v.txExecutorBackend,
		ParentID:      parentID,
		StateVersions: v.backend,
		Tx:            b.Tx,
	}

	if err := b.Tx.Unsigned.Visit(atomicExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(b.Tx, status.Committed)

	// Check for conflicts in atomic inputs.
	if len(atomicExecutor.Inputs) > 0 {
		var nextBlock blocks.Block = b
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
			nextBlock = parentState.statelessBlock
		}
	}

	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  atomicExecutor.OnAccept,
		standardBlockState: standardBlockState{
			inputs: atomicExecutor.Inputs,
		},
		atomicRequests: atomicExecutor.AtomicRequests,
		timestamp:      atomicExecutor.OnAccept.GetTimestamp(),
	}
	v.blkIDToState[blkID] = blkState
	v.Mempool.RemoveDecisionTxs([]*txs.Tx{b.Tx})
	return nil
}

// verifyUniqueInputs verifies that the inputs of the given block are not
// duplicated in any of the parent blocks pinned in memory.
func (v *verifier) verifyUniqueInputs(block blocks.Block, inputs ids.Set) error {
	if inputs.Len() == 0 {
		return nil
	}

	// Check for conflicts in ancestors.
	for {
		parentID := block.Parent()
		parentState, ok := v.blkIDToState[parentID]
		if !ok {
			// The parent state isn't pinned in memory.
			// This means the parent must be accepted already.
			return nil
		}

		if parentState.inputs.Overlaps(inputs) {
			return errConflictingParentTxs
		}

		block = parentState.statelessBlock
	}
}
