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

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	tx := b.BlockTxs()[0]
	txExecutor := executor.ProposalTxExecutor{
		Backend:          &v.txExecutorBackend,
		ReferenceBlockID: b.Parent(),
		Tx:               tx,
	}

	if err := tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	onCommitState := txExecutor.OnCommit
	onCommitState.AddTx(b.Tx, status.Committed)

	onAbortState := txExecutor.OnAbort
	onAbortState.AddTx(b.Tx, status.Aborted)

	blkState := &blockState{
		statelessBlock: b,
		proposalBlockState: proposalBlockState{
			onCommitState:         onCommitState,
			onAbortState:          onAbortState,
			initiallyPreferCommit: txExecutor.PrefersCommit,
		},

		// It is safe to use [pb.onAbortState] here because the timestamp will never
		// be modified by an Abort block.
		timestamp: onAbortState.GetTimestamp(),
	}
	v.blkIDToState[blkID] = blkState

	// Notice that we do not add an entry to the state versions here for this
	// block. This block must be followed by either a Commit or an Abort block.
	// These blocks will get their parent state by referencing [onCommitState]
	// or [onAbortState] directly.

	v.Mempool.RemoveProposalTx(b.Tx)
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

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	tx := b.BlockTxs()[0]
	if _, ok := tx.Unsigned.(*txs.AdvanceTimeTx); ok {
		return errAdvanceTimeTxCannotBeIncluded
	}

	parentID := b.Parent()
	parentState, ok := v.stateVersions.GetState(parentID)
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}
	nextChainTime := time.Unix(b.UnixTimestamp(), 0)

	// Having verifier block timestamp, we update staker set
	// before processing block transaction
	currentValidatorsToAdd,
		currentValidatorsToRemove,
		pendingValidatorsToRemove,
		currentDelegatorsToAdd,
		pendingDelegatorsToRemove,
		updatedSupply,
		err := executor.UpdateStakerSet(parentState, &v.txExecutorBackend, nextChainTime)
	if err != nil {
		return err
	}

	onAcceptState, err := state.NewDiff(parentID, v.stateVersions)
	if err != nil {
		return err
	}
	onAcceptState.SetTimestamp(nextChainTime)
	onAcceptState.SetCurrentSupply(updatedSupply)

	for _, currentValidatorToAdd := range currentValidatorsToAdd {
		onAcceptState.PutCurrentValidator(currentValidatorToAdd)
	}
	for _, pendingValidatorToRemove := range pendingValidatorsToRemove {
		onAcceptState.DeletePendingValidator(pendingValidatorToRemove)
	}
	for _, currentDelegatorToAdd := range currentDelegatorsToAdd {
		onAcceptState.PutCurrentDelegator(currentDelegatorToAdd)
	}
	for _, pendingDelegatorToRemove := range pendingDelegatorsToRemove {
		onAcceptState.DeletePendingDelegator(pendingDelegatorToRemove)
	}
	for _, currentValidatorToRemove := range currentValidatorsToRemove {
		onAcceptState.DeleteCurrentValidator(currentValidatorToRemove)
	}
	blkState.onAcceptState = onAcceptState

	// Finally we process block transaction
	// Unlike ApricotProposalBlock,we register an entry for BlueberryProposalBlock in stateVersions.
	// We do it before processing the proposal block tx, since it needs the state for correct verification.
	v.stateVersions.SetState(blkID, onAcceptState)

	txExecutor := executor.ProposalTxExecutor{
		Backend:          &v.txExecutorBackend,
		ReferenceBlockID: blkID,
		Tx:               tx,
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

	v.blkIDToState[blkID] = blkState
	v.Mempool.RemoveProposalTx(b.Tx)
	return nil
}

func (v *verifier) VisitAtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.stateVersions.GetState(parentID)
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
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

	tx := b.BlockTxs()[0]
	atomicExecutor := executor.AtomicTxExecutor{
		Backend:  &v.txExecutorBackend,
		ParentID: parentID,
		Tx:       tx,
	}

	if err := tx.Unsigned.Visit(&atomicExecutor); err != nil {
		txID := tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(tx, status.Committed)

	// Check for conflicts in atomic inputs.
	if len(atomicExecutor.Inputs) > 0 {
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
	}

	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  atomicExecutor.OnAccept,
		atomicBlockState: atomicBlockState{
			inputs: atomicExecutor.Inputs,
		},
		atomicRequests: atomicExecutor.AtomicRequests,
		timestamp:      atomicExecutor.OnAccept.GetTimestamp(),
	}
	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	v.Mempool.RemoveDecisionTxs([]*txs.Tx{b.Tx})
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

	onAcceptState, err := state.NewDiff(
		b.Parent(),
		v.stateVersions,
	)
	if err != nil {
		return err
	}

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

	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	v.Mempool.RemoveDecisionTxs(b.Txs)
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
	parentState, ok := v.stateVersions.GetState(parentID)
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}
	nextChainTime := time.Unix(b.UnixTimestamp(), 0)

	// Having verifier block timestamp, we update staker set
	// before processing block transaction
	currentValidatorsToAdd,
		currentValidatorsToRemove,
		pendingValidatorsToRemove,
		currentDelegatorsToAdd,
		pendingDelegatorsToRemove,
		updatedSupply,
		err := executor.UpdateStakerSet(parentState, &v.txExecutorBackend, nextChainTime)
	if err != nil {
		return err
	}

	onAcceptState, err := state.NewDiff(parentID, v.stateVersions)
	if err != nil {
		return err
	}
	onAcceptState.SetTimestamp(nextChainTime)
	onAcceptState.SetCurrentSupply(updatedSupply)

	for _, currentValidatorToAdd := range currentValidatorsToAdd {
		onAcceptState.PutCurrentValidator(currentValidatorToAdd)
	}
	for _, pendingValidatorToRemove := range pendingValidatorsToRemove {
		onAcceptState.DeletePendingValidator(pendingValidatorToRemove)
	}
	for _, currentDelegatorToAdd := range currentDelegatorsToAdd {
		onAcceptState.PutCurrentDelegator(currentDelegatorToAdd)
	}
	for _, pendingDelegatorToRemove := range pendingDelegatorsToRemove {
		onAcceptState.DeletePendingDelegator(pendingDelegatorToRemove)
	}
	for _, currentValidatorToRemove := range currentValidatorsToRemove {
		onAcceptState.DeleteCurrentValidator(currentValidatorToRemove)
	}

	// Finally we process block transaction
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

	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	v.Mempool.RemoveDecisionTxs(b.Txs)
	return nil
}

func (v *verifier) VisitCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return fmt.Errorf("couldn't verify common block of %s: %s", blkID, err)
	}

	blkVersion := b.Version()
	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}
	onAcceptState := parentState.onCommitState

	var blkStateTime time.Time
	if blkVersion == stateless.BlueberryVersion {
		if err := v.validateBlockTimestamp(
			b,
			parentState.timestamp,
		); err != nil {
			return err
		}
		blkStateTime = time.Unix(b.UnixTimestamp(), 0)
	} else if blkVersion == stateless.ApricotVersion {
		blkStateTime = onAcceptState.GetTimestamp()
	}

	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  onAcceptState,
		timestamp:      blkStateTime,
	}
	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	return nil
}

func (v *verifier) VisitAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	parentID := b.Parent()
	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	blkVersion := b.Version()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}
	onAcceptState := parentState.onAbortState

	var blkStateTime time.Time
	if blkVersion == stateless.BlueberryVersion {
		if err := v.validateBlockTimestamp(
			b,
			parentState.timestamp,
		); err != nil {
			return err
		}
		blkStateTime = time.Unix(b.UnixTimestamp(), 0)
	} else if blkVersion == stateless.ApricotVersion {
		blkStateTime = onAcceptState.GetTimestamp()
	}

	blkState := &blockState{
		statelessBlock: b,
		timestamp:      blkStateTime,
		onAcceptState:  onAcceptState,
	}
	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
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
	parentTimestamp := parentBlk.Timestamp()
	expectedVersion := v.expectedChildVersion(parentTimestamp)
	if expectedVersion != blkVersion {
		return fmt.Errorf(
			"expected block to have version %d, but found %d",
			expectedVersion,
			blkVersion,
		)
	}

	return v.validateBlockTimestamp(b, parentTimestamp)
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
		parentID := blk.Parent()
		parentState, ok := v.blkIDToState[parentID]
		if !ok {
			return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blk.ID())
		}
		nextStakerChangeTime, err := executor.GetNextStakerChangeTime(parentState.onAcceptState)
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
