// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ verifier = &verifierImpl{}

type verifier interface {
	// Verify this block is valid.
	// The parent block must either be a Commit or an Abort block.
	// If this block is valid, this function also sets pas.onCommit and pas.onAbort.
	verifyProposalBlock(b *ProposalBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a decision block
	// This function also sets onAcceptDB database if the verification passes.
	verifyAtomicBlock(b *AtomicBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a proposal
	// This function also sets onAcceptDB database if the verification passes.
	verifyStandardBlock(b *StandardBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a proposal
	// This function also sets onAcceptState if the verification passes.
	verifyCommitBlock(b *CommitBlock) error

	// Verify this block performs a valid state transition.
	// The parent block must be a proposal
	// This function also sets onAcceptState if the verification passes.
	verifyAbortBlock(b *AbortBlock) error
}

type verifierImpl struct {
	backend
	txExecutorBackend executor.Backend
}

func (v *verifierImpl) verifyProposalBlock(b *ProposalBlock) error {
	if err := v.verifyCommonBlock(b.commonBlock, false /*enforceStrictness*/); err != nil {
		return err
	}

	if b.Version() == stateless.BlueberryVersion {
		if _, ok := b.ProposalTx().Unsigned.(*txs.AdvanceTimeTx); ok {
			return ErrAdvanceTimeTxCannotBeIncluded
		}
	}

	parentIntf, parentErr := v.parent(b.baseBlk)
	if parentErr != nil {
		return parentErr
	}

	// parentState is the state if this block's parent is accepted
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected *DecisionBlock but got %T", parentIntf)
	}

	parentState := parent.OnAccept()
	tx := b.ProposalTx()
	blkVersion := b.Version()
	var txExecutor executor.ProposalTxExecutor

	switch blkVersion {
	case stateless.ApricotVersion:
		txExecutor = executor.ProposalTxExecutor{
			Backend:     &v.txExecutorBackend,
			ParentState: parentState,
			Tx:          tx,
		}

	case stateless.BlueberryVersion:
		// Having verifier block timestamp, we update staker set
		// before processing block transaction
		var (
			newlyCurrentStakers state.CurrentStakers
			newlyPendingStakers state.PendingStakers
			updatedSupply       uint64
		)
		nextChainTime := b.Timestamp()
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

		b.onBlueberryBaseOptionsState = baseOptionsState
		txExecutor = executor.ProposalTxExecutor{
			Backend:     &v.txExecutorBackend,
			ParentState: baseOptionsState,
			Tx:          tx,
		}

	default:
		return fmt.Errorf("block version %d, unknown recipe to update chain state. Verification failed", blkVersion)
	}

	if err := tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	b.onCommitState = txExecutor.OnCommit
	b.onAbortState = txExecutor.OnAbort
	b.prefersCommit = txExecutor.PrefersCommit

	b.onCommitState.AddTx(tx, status.Committed)
	b.onAbortState.AddTx(tx, status.Aborted)

	b.SetTimestamp(parentState.GetTimestamp())

	v.Mempool.RemoveProposalTx(tx)
	v.pinVerifiedBlock(b)
	parentIntf.addChild(b)
	return nil
}

func (v *verifierImpl) verifyAtomicBlock(b *AtomicBlock) error {
	if err := v.verifyCommonBlock(b.commonBlock, true /*enforceStrictness*/); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}

	// AtomicBlock is not a modifier on a proposal block, so its parent must be
	// a decision.
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}

	parentState := parent.OnAccept()

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

	tx := b.AtomicTx()
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

	b.onAcceptState = atomicExecutor.OnAccept
	b.inputs = atomicExecutor.Inputs
	b.atomicRequests = atomicExecutor.AtomicRequests
	b.SetTimestamp(atomicExecutor.OnAccept.GetTimestamp())

	conflicts, err := parentIntf.conflicts(b.inputs)
	if err != nil {
		return err
	}
	if conflicts {
		return ErrConflictingParentTxs
	}

	v.Mempool.RemoveDecisionTxs([]*txs.Tx{tx})
	v.pinVerifiedBlock(b)
	parentIntf.addChild(b)
	return nil
}

func (v *verifierImpl) verifyStandardBlock(b *StandardBlock) error {
	if err := v.verifyCommonBlock(b.commonBlock, false /*enforceStrictness*/); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}

	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}
	parentState := parent.OnAccept()

	blkVersion := b.Version()
	switch blkVersion {
	case stateless.ApricotVersion:
		b.onAcceptState = state.NewDiff(
			parentState,
			parentState.CurrentStakers(),
			parentState.PendingStakers(),
		)

	case stateless.BlueberryVersion:
		// We update staker set before processing block transactions
		nextChainTime := b.Timestamp()
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

		b.onAcceptState = state.NewDiff(
			parentState,
			newlyCurrentStakers,
			newlyPendingStakers,
		)
		b.onAcceptState.SetTimestamp(nextChainTime)
		b.onAcceptState.SetCurrentSupply(updatedSupply)

	default:
		return fmt.Errorf(
			"block version %d, unknown recipe to update chain state. Verification failed",
			blkVersion,
		)
	}

	// clear inputs so that multiple [Verify] calls can be made
	b.Inputs.Clear()
	b.atomicRequests = make(map[ids.ID]*atomic.Requests)

	txes := b.DecisionTxs()
	funcs := make([]func(), 0, len(txes))
	for _, tx := range txes {
		txExecutor := executor.StandardTxExecutor{
			Backend: &v.txExecutorBackend,
			State:   b.onAcceptState,
			Tx:      tx,
		}
		if err := tx.Unsigned.Visit(&txExecutor); err != nil {
			txID := tx.ID()
			v.MarkDropped(txID, err.Error()) // cache tx as dropped
			return err
		}
		// ensure it doesn't overlap with current input batch
		if b.Inputs.Overlaps(txExecutor.Inputs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		b.Inputs.Union(txExecutor.Inputs)

		b.onAcceptState.AddTx(tx, status.Committed)
		if txExecutor.OnAccept != nil {
			funcs = append(funcs, txExecutor.OnAccept)
		}

		for chainID, txRequests := range txExecutor.AtomicRequests {
			// Add/merge in the atomic requests represented by [tx]
			chainRequests, exists := b.atomicRequests[chainID]
			if !exists {
				b.atomicRequests[chainID] = txRequests
				continue
			}

			chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
			chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
		}
	}

	if b.Inputs.Len() > 0 {
		// ensure it doesnt conflict with the parent block
		conflicts, err := parentIntf.conflicts(b.Inputs)
		if err != nil {
			return err
		}
		if conflicts {
			return ErrConflictingParentTxs
		}
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		b.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		b.onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	b.SetTimestamp(b.onAcceptState.GetTimestamp())

	v.Mempool.RemoveDecisionTxs(txes)
	v.pinVerifiedBlock(b)
	parentIntf.addChild(b)
	return nil
}

func (v *verifierImpl) verifyCommitBlock(b *CommitBlock) error {
	if err := v.verifyCommonBlock(b.decisionBlock.commonBlock, false /*enforceStrictness*/); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}

	// The parent of a Commit block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	b.onAcceptState = parent.onCommitState
	b.SetTimestamp(b.onAcceptState.GetTimestamp())

	v.pinVerifiedBlock(b)
	parent.addChild(b)
	return nil
}

func (v *verifierImpl) verifyAbortBlock(b *AbortBlock) error {
	if err := v.verifyCommonBlock(b.decisionBlock.commonBlock, false /*enforceStrictness*/); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}

	// The parent of an Abort block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	b.onAcceptState = parent.onAbortState
	b.SetTimestamp(b.onAcceptState.GetTimestamp())

	v.pinVerifiedBlock(b)
	parent.addChild(b)
	return nil
}

// Assumes [b] isn't nil
func (v *verifierImpl) verifyCommonBlock(cb *commonBlock, enforceStrictness bool) error {
	parent, err := v.parent(cb.baseBlk)
	if err != nil {
		return err
	}

	// verify block height
	expectedHeight := parent.Height() + 1
	if expectedHeight != cb.baseBlk.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			cb.baseBlk.Height(),
		)
	}

	// verify block version
	blkVersion := cb.baseBlk.Version()
	expectedVersion := parent.ExpectedChildVersion()
	if expectedVersion != blkVersion {
		return fmt.Errorf(
			"expected block to have version %d, but found %d",
			expectedVersion,
			blkVersion,
		)
	}

	return v.validateBlockTimestamp(cb, enforceStrictness)
}

func (v *verifierImpl) validateBlockTimestamp(cb *commonBlock, enforceStrictness bool) error {
	// verify timestamp only for post apricot blocks
	// Note: atomic blocks have been deprecated before blueberry fork activation,
	// therefore validateBlockTimestamp for atomic blocks should return
	// immediately as they should all have ApricotVersion.
	// We do not bother distinguishing atomic blocks below.
	if cb.baseBlk.Version() == stateless.ApricotVersion {
		return nil
	}

	parentBlk, err := v.parent(cb.baseBlk)
	if err != nil {
		return err
	}

	blkTime := cb.Timestamp()
	currentChainTime := parentBlk.Timestamp()

	switch cb.baseBlk.(type) {
	case *stateless.AbortBlock,
		*stateless.CommitBlock:
		if !blkTime.Equal(currentChainTime) {
			return fmt.Errorf(
				"%w parent block timestamp (%s) option block timestamp (%s)",
				ErrOptionBlockTimestampNotMatchingParent,
				currentChainTime,
				blkTime,
			)
		}
		return nil

	case *stateless.ApricotStandardBlock,
		*stateless.ApricotProposalBlock,
		*stateless.BlueberryStandardBlock,
		*stateless.BlueberryProposalBlock:
		parentDecision, ok := parentBlk.(Decision)
		if !ok {
			// The preferred block should always be a decision block
			return fmt.Errorf("expected Decision block but got %T", parentBlk)
		}
		parentState := parentDecision.OnAccept()
		nextStakerChangeTime, err := parentState.GetNextStakerChangeTime()
		if err != nil {
			return fmt.Errorf("could not verify block timestamp: %w", err)
		}
		localTime := v.txExecutorBackend.Clk.Time()

		return executor.ValidateProposedChainTime(
			blkTime,
			currentChainTime,
			nextStakerChangeTime,
			localTime,
			enforceStrictness,
		)

	default:
		return fmt.Errorf(
			"cannot not validate block timestamp for block type %T",
			cb.baseBlk,
		)
	}
}
