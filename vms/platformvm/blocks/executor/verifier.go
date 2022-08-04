// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor/version"
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
	man               *manager
	txExecutorBackend executor.Backend
}

func (v *verifier) ApricotProposalBlock(b *blocks.ApricotProposalBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	txExecutor := &executor.ProposalTxExecutor{
		Backend:          &v.txExecutorBackend,
		ReferenceBlockID: b.Parent(),
		Tx:               b.Tx,
	}
	if err := b.Tx.Unsigned.Visit(txExecutor); err != nil {
		txID := b.Tx.ID()
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

		// It is safe to use [pb.onAbortState] here because the timestamp will
		// never be modified by an Abort block.
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

func (v *verifier) BlueberryProposalBlock(b *blocks.BlueberryProposalBlock) error {
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

	if _, ok := b.Tx.Unsigned.(*txs.AdvanceTimeTx); ok {
		return errAdvanceTimeTxCannotBeIncluded
	}

	parentID := b.Parent()
	parentState, ok := v.stateVersions.GetState(parentID)
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}
	nextChainTime := b.BlockTimestamp()

	// Having verifier block timestamp, we update staker set
	// before processing block transaction
	currentValidatorsToAdd,
		currentValidatorsToRemove,
		pendingValidatorsToRemove,
		currentDelegatorsToAdd,
		pendingDelegatorsToRemove,
		updatedSupply,
		err := executor.UpdateStakerSet(parentState, nextChainTime, v.txExecutorBackend.Rewards)
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
		Tx:               b.Tx,
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

	blkState.timestamp = b.BlockTimestamp()
	blkState.initiallyPreferCommit = txExecutor.PrefersCommit

	v.blkIDToState[blkID] = blkState
	v.Mempool.RemoveProposalTx(b.Tx)
	return nil
}

func (v *verifier) AtomicBlock(b *blocks.AtomicBlock) error {
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

	atomicExecutor := &executor.AtomicTxExecutor{
		Backend:  &v.txExecutorBackend,
		ParentID: parentID,
		Tx:       b.Tx,
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
		standardBlockState: standardBlockState{
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

	funcs := make([]func(), 0, len(b.Transactions))
	for _, tx := range b.Transactions {
		txExecutor := &executor.StandardTxExecutor{
			Backend: &v.txExecutorBackend,
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
	blkState.timestamp = onAcceptState.GetTimestamp()
	blkState.onAcceptState = onAcceptState

	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	v.Mempool.RemoveDecisionTxs(b.Transactions)
	return nil
}

func (v *verifier) BlueberryStandardBlock(b *blocks.BlueberryStandardBlock) error {
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
	nextChainTime := b.BlockTimestamp()

	// Having verifier block timestamp, we update staker set
	// before processing block transaction
	currentValidatorsToAdd,
		currentValidatorsToRemove,
		pendingValidatorsToRemove,
		currentDelegatorsToAdd,
		pendingDelegatorsToRemove,
		updatedSupply,
		err := executor.UpdateStakerSet(parentState, nextChainTime, v.txExecutorBackend.Rewards)
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
	funcs := make([]func(), 0, len(b.Transactions))
	for _, tx := range b.Transactions {
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
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	v.Mempool.RemoveDecisionTxs(b.Transactions)
	return nil
}

func (v *verifier) CommitBlock(b *blocks.CommitBlock) error {
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

	var blkTimestamp time.Time
	switch blkVersion {
	case version.BlueberryBlockVersion:
		if err := v.validateBlockTimestamp(
			b,
			parentState.timestamp,
		); err != nil {
			return err
		}
		blkTimestamp = b.BlockTimestamp()
	case version.ApricotBlockVersion:
		blkTimestamp = onAcceptState.GetTimestamp()
	default:
		return fmt.Errorf("invalid block version: %d", blkVersion)
	}

	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  onAcceptState,
		timestamp:      blkTimestamp,
	}
	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	return nil
}

func (v *verifier) AbortBlock(b *blocks.AbortBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}
	onAcceptState := parentState.onAbortState

	var blkTimestamp time.Time
	switch b.Version() {
	case version.BlueberryBlockVersion:
		if err := v.validateBlockTimestamp(b, parentState.timestamp); err != nil {
			return err
		}
		blkTimestamp = b.BlockTimestamp()

	case blocks.ApricotVersion:
		blkTimestamp = onAcceptState.GetTimestamp()

	default:
		return fmt.Errorf("invalid block version: %d", b.Version())
	}
	blkState := &blockState{
		statelessBlock: b,
		timestamp:      blkTimestamp,
		onAcceptState:  onAcceptState,
	}
	v.blkIDToState[blkID] = blkState
	v.stateVersions.SetState(blkID, blkState.onAcceptState)
	return nil
}

func (v *verifier) verifyCommonBlock(b blocks.Block) error {
	// retrieve parent block first
	parentID := b.Parent()
	parentBlk, err := v.getStatelessBlock(parentID)
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
	// We need the parent's timestamp.
	// Verify was already called on the parent (guaranteed by consensus engine).
	// The parent hasn't been rejected (guaranteed by consensus engine).
	// If the parent is accepted, the parent is the most recently
	// accepted block.
	// If the parent hasn't been accepted, the parent is in memory.
	var parentTimestamp time.Time
	if parentState, ok := v.blkIDToState[parentID]; ok {
		parentTimestamp = parentState.timestamp
	} else {
		parentTimestamp = v.state.GetTimestamp()
	}
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

func (v *verifier) validateBlockTimestamp(blk blocks.Block, parentBlkTime time.Time) error {
	if blk.Version() == blocks.ApricotVersion {
		return nil
	}

	blkTime := blk.BlockTimestamp()

	switch blk.(type) {
	case *blocks.AbortBlock,
		*blocks.CommitBlock:
		if !blkTime.Equal(parentBlkTime) {
			return fmt.Errorf(
				"%w parent block timestamp (%s) option block timestamp (%s)",
				errOptionBlockTimestampNotMatchingParent,
				parentBlkTime,
				blkTime,
			)
		}
		return nil

	case *blocks.BlueberryStandardBlock,
		*blocks.BlueberryProposalBlock:
		parentID := blk.Parent()
		parentState, ok := v.stateVersions.GetState(parentID)
		if !ok {
			return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blk.ID())
		}
		nextStakerChangeTime, err := executor.GetNextStakerChangeTime(parentState)
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
