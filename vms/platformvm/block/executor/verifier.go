// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	_ block.Visitor = (*verifier)(nil)

	errApricotBlockIssuedAfterFork                = errors.New("apricot block issued after fork")
	errBanffProposalBlockWithMultipleTransactions = errors.New("BanffProposalBlock contains multiple transactions")
	errBanffStandardBlockWithoutChanges           = errors.New("BanffStandardBlock performs no state changes")
	errIncorrectBlockHeight                       = errors.New("incorrect block height")
	errChildBlockEarlierThanParent                = errors.New("proposed timestamp before current chain time")
	errConflictingBatchTxs                        = errors.New("block contains conflicting transactions")
	errOptionBlockTimestampNotMatchingParent      = errors.New("option block proposed timestamp not matching parent block one")
)

// verifier handles the logic for verifying a block.
type verifier struct {
	*backend
	txExecutorBackend *executor.Backend
}

func (v *verifier) BanffAbortBlock(b *block.BanffAbortBlock) error {
	if err := v.banffOptionBlock(b); err != nil {
		return err
	}
	return v.abortBlock(b)
}

func (v *verifier) BanffCommitBlock(b *block.BanffCommitBlock) error {
	if err := v.banffOptionBlock(b); err != nil {
		return err
	}
	return v.commitBlock(b)
}

func (v *verifier) BanffProposalBlock(b *block.BanffProposalBlock) error {
	if len(b.Transactions) != 0 {
		return errBanffProposalBlockWithMultipleTransactions
	}

	if err := v.banffNonOptionBlock(b); err != nil {
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

	// Apply the changes, if any, from advancing the chain time.
	nextChainTime := b.Timestamp()
	changes, err := executor.AdvanceTimeTo(
		v.txExecutorBackend,
		onCommitState,
		nextChainTime,
	)
	if err != nil {
		return err
	}

	onCommitState.SetTimestamp(nextChainTime)
	changes.Apply(onCommitState)

	onAbortState.SetTimestamp(nextChainTime)
	changes.Apply(onAbortState)

	return v.proposalBlock(&b.ApricotProposalBlock, onCommitState, onAbortState)
}

func (v *verifier) BanffStandardBlock(b *block.BanffStandardBlock) error {
	if err := v.banffNonOptionBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onAcceptState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	// Apply the changes, if any, from advancing the chain time.
	nextChainTime := b.Timestamp()
	changes, err := executor.AdvanceTimeTo(
		v.txExecutorBackend,
		onAcceptState,
		nextChainTime,
	)
	if err != nil {
		return err
	}

	// If this block doesn't perform any changes, then it should never have been
	// issued.
	if changes.Len() == 0 && len(b.Transactions) == 0 {
		return errBanffStandardBlockWithoutChanges
	}

	onAcceptState.SetTimestamp(nextChainTime)
	changes.Apply(onAcceptState)

	return v.standardBlock(&b.ApricotStandardBlock, onAcceptState)
}

func (v *verifier) ApricotAbortBlock(b *block.ApricotAbortBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}
	return v.abortBlock(b)
}

func (v *verifier) ApricotCommitBlock(b *block.ApricotCommitBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}
	return v.commitBlock(b)
}

func (v *verifier) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
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

	return v.proposalBlock(b, onCommitState, onAbortState)
}

func (v *verifier) ApricotStandardBlock(b *block.ApricotStandardBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onAcceptState, err := state.NewDiff(parentID, v)
	if err != nil {
		return err
	}

	return v.standardBlock(b, onAcceptState)
}

func (v *verifier) ApricotAtomicBlock(b *block.ApricotAtomicBlock) error {
	// We call [commonBlock] here rather than [apricotCommonBlock] because below
	// this check we perform the more strict check that ApricotPhase5 isn't
	// activated.
	if err := v.commonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	currentTimestamp := v.getTimestamp(parentID)
	cfg := v.txExecutorBackend.Config
	if cfg.IsApricotPhase5Activated(currentTimestamp) {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			cfg.ApricotPhase5Time.Unix(),
		)
	}

	atomicExecutor := executor.AtomicTxExecutor{
		Backend:       v.txExecutorBackend,
		ParentID:      parentID,
		StateVersions: v,
		Tx:            b.Tx,
	}

	if err := b.Tx.Unsigned.Visit(&atomicExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(b.Tx, status.Committed)

	if err := v.verifyUniqueInputs(b.Parent(), atomicExecutor.Inputs); err != nil {
		return err
	}

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,

		onAcceptState: atomicExecutor.OnAccept,

		inputs:         atomicExecutor.Inputs,
		timestamp:      atomicExecutor.OnAccept.GetTimestamp(),
		atomicRequests: atomicExecutor.AtomicRequests,
	}

	v.Mempool.Remove([]*txs.Tx{b.Tx})
	return nil
}

func (v *verifier) banffOptionBlock(b block.BanffBlock) error {
	if err := v.commonBlock(b); err != nil {
		return err
	}

	// Banff option blocks must be uniquely generated from the
	// BanffProposalBlock. This means that the timestamp must be
	// standardized to a specific value. Therefore, we require the timestamp to
	// be equal to the parents timestamp.
	parentID := b.Parent()
	parentBlkTime := v.getTimestamp(parentID)
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

func (v *verifier) banffNonOptionBlock(b block.BanffBlock) error {
	if err := v.commonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	newChainTime := b.Timestamp()
	parentChainTime := parentState.GetTimestamp()
	if newChainTime.Before(parentChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s), chain time (%s)",
			errChildBlockEarlierThanParent,
			newChainTime,
			parentChainTime,
		)
	}

	nextStakerChangeTime, err := executor.GetNextStakerChangeTime(parentState)
	if err != nil {
		return fmt.Errorf("could not verify block timestamp: %w", err)
	}

	now := v.txExecutorBackend.Clk.Time()
	return executor.VerifyNewChainTime(
		newChainTime,
		nextStakerChangeTime,
		now,
	)
}

func (v *verifier) apricotCommonBlock(b block.Block) error {
	// We can use the parent timestamp here, because we are guaranteed that the
	// parent was verified. Apricot blocks only update the timestamp with
	// AdvanceTimeTxs. This means that this block's timestamp will be equal to
	// the parent block's timestamp; unless this is a CommitBlock. In order for
	// the timestamp of the CommitBlock to be after the Banff activation,
	// the parent ApricotProposalBlock must include an AdvanceTimeTx with a
	// timestamp after the Banff timestamp. This is verified not to occur
	// during the verification of the ProposalBlock.
	parentID := b.Parent()
	timestamp := v.getTimestamp(parentID)
	if v.txExecutorBackend.Config.IsBanffActivated(timestamp) {
		return fmt.Errorf("%w: timestamp = %s", errApricotBlockIssuedAfterFork, timestamp)
	}
	return v.commonBlock(b)
}

func (v *verifier) commonBlock(b block.Block) error {
	parentID := b.Parent()
	parent, err := v.GetBlock(parentID)
	if err != nil {
		return err
	}

	expectedHeight := parent.Height() + 1
	height := b.Height()
	if expectedHeight != height {
		return fmt.Errorf(
			"%w expected %d, but found %d",
			errIncorrectBlockHeight,
			expectedHeight,
			height,
		)
	}
	return nil
}

// abortBlock populates the state of this block if [nil] is returned
func (v *verifier) abortBlock(b block.Block) error {
	parentID := b.Parent()
	parentState, ok := v.getBlkWithOnAbortState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,

		onAcceptState: parentState.onAbortState,
		onAcceptFunc:  parentState.onAcceptFunc,

		inputs:         parentState.inputs,
		timestamp:      parentState.onAbortState.GetTimestamp(),
		atomicRequests: parentState.atomicRequests,
	}
	return nil
}

// commitBlock populates the state of this block if [nil] is returned
func (v *verifier) commitBlock(b block.Block) error {
	parentID := b.Parent()
	parentState, ok := v.getBlkWithOnCommitState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,

		onAcceptState: parentState.onCommitState,
		onAcceptFunc:  parentState.onAcceptFunc,

		inputs:         parentState.inputs,
		timestamp:      parentState.onCommitState.GetTimestamp(),
		atomicRequests: parentState.atomicRequests,
	}
	return nil
}

// proposalBlock populates the state of this block if [nil] is returned
func (v *verifier) proposalBlock(
	b *block.ApricotProposalBlock,
	onCommitState state.Diff,
	onAbortState state.Diff,
) error {
	txExecutor := executor.ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       v.txExecutorBackend,
		Tx:            b.Tx,
	}

	if err := b.Tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err) // cache tx as dropped
		return err
	}

	onCommitState.AddTx(b.Tx, status.Committed)
	onAbortState.AddTx(b.Tx, status.Aborted)

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		proposalBlockState: proposalBlockState{
			onCommitState:         onCommitState,
			onAbortState:          onAbortState,
			initiallyPreferCommit: txExecutor.PrefersCommit,
		},
		statelessBlock: b,
		// It is safe to use [b.onAbortState] here because the timestamp will
		// never be modified by an Apricot Abort block and the timestamp will
		// always be the same as the Banff Proposal Block.
		timestamp: onAbortState.GetTimestamp(),
	}

	v.Mempool.Remove([]*txs.Tx{b.Tx})
	return nil
}

// standardBlock populates the state of this block if [nil] is returned
func (v *verifier) standardBlock(
	b *block.ApricotStandardBlock,
	onAcceptState state.Diff,
) error {
	blkState := &blockState{
		statelessBlock: b,
		onAcceptState:  onAcceptState,
		timestamp:      onAcceptState.GetTimestamp(),
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	// Finally we process the transactions
	funcs := make([]func(), 0, len(b.Transactions))
	for _, tx := range b.Transactions {
		txExecutor := executor.StandardTxExecutor{
			Backend: v.txExecutorBackend,
			State:   onAcceptState,
			Tx:      tx,
		}
		if err := tx.Unsigned.Visit(&txExecutor); err != nil {
			txID := tx.ID()
			v.MarkDropped(txID, err) // cache tx as dropped
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

	if err := v.verifyUniqueInputs(b.Parent(), blkState.inputs); err != nil {
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

	blkID := b.ID()
	v.blkIDToState[blkID] = blkState

	v.Mempool.Remove(b.Transactions)
	return nil
}
