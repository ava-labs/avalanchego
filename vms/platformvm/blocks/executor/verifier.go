// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
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
	forkChecker       *forkChecker
}

func (v *verifier) BlueberryAbortBlock(b *blocks.BlueberryAbortBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.blueberryCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}

	blkState := &blockState{
		statelessBlock: b,
		timestamp:      b.BlockTimestamp(),
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

	if err := v.blueberryCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}

	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  parentState.onCommitState,
		timestamp:      b.BlockTimestamp(),
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

	if err := v.blueberryCommonBlock(b); err != nil {
		return err
	}

	if _, ok := b.Tx.Unsigned.(*txs.AdvanceTimeTx); ok {
		return errAdvanceTimeTxCannotBeIncluded
	}

	blkState := &blockState{
		statelessBlock: b,
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
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

	onAcceptState, err := state.NewDiff(parentID, v.backend)
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
	v.blkIDToState[blkID] = blkState

	txExecutor := executor.ProposalTxExecutor{
		Backend:          v.txExecutorBackend,
		ReferenceBlockID: blkID,
		StateVersions:    v.backend,
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

func (v *verifier) BlueberryStandardBlock(b *blocks.BlueberryStandardBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.blueberryCommonBlock(b); err != nil {
		return err
	}

	blkState := &blockState{
		statelessBlock: b,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
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

	onAcceptState, err := state.NewDiff(parentID, v.backend)
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

	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
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

	if err := v.apricotCommonBlock(b); err != nil {
		return fmt.Errorf("couldn't verify common block of %s: %s", blkID, err)
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
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

	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}

	txExecutor := &executor.ProposalTxExecutor{
		Backend:          v.txExecutorBackend,
		ReferenceBlockID: b.Parent(),
		StateVersions:    v.backend,
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

	v.blkIDToState[blkID] = &blockState{
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

	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}

	onAcceptState, err := state.NewDiff(b.Parent(), v)
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

func (v *verifier) blueberryCommonBlock(b blocks.Block) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}
	return v.validateBlockTimestamp(b)
}

func (v *verifier) apricotCommonBlock(b blocks.Block) error {
	parentID := b.Parent()
	parent, err := v.GetBlock(parentID)
	if err != nil {
		return err
	}

	if expectedHeight := parent.Height() + 1; expectedHeight != b.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			b.Height(),
		)
	}

	// check whether block type is allowed in current fork
	return b.Visit(v.forkChecker)
}

func (v *verifier) validateBlockTimestamp(b blocks.Block) error {
	parentID := b.Parent()
	parentBlk, err := v.getStatelessBlock(parentID)
	if err != nil {
		return err
	}
	parentBlkTime := parentBlk.BlockTimestamp()
	blkTime := b.BlockTimestamp()

	switch b.(type) {
	case *blocks.BlueberryAbortBlock,
		*blocks.BlueberryCommitBlock:
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
		parentID := b.Parent()
		parentState, ok := v.GetState(parentID)
		if !ok {
			return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, b.ID())
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
		return fmt.Errorf("cannot not validate block timestamp for block type %T", b)
	}
}

func (v *verifier) ApricotAtomicBlock(b *blocks.ApricotAtomicBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
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
