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

	errConflictingBatchTxs  = errors.New("block contains conflicting transactions")
	errConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
)

// verifier handles the logic for verifying a block.
type verifier struct {
	*backend
	txExecutorBackend *executor.Backend
}

func (v *verifier) ProposalBlock(b *blocks.ProposalBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(&b.CommonBlock); err != nil {
		return err
	}

	txExecutor := &executor.ProposalTxExecutor{
		Backend:       v.txExecutorBackend,
		ParentID:      b.Parent(),
		StateVersions: v,
		Tx:            b.Tx,
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

func (v *verifier) AtomicBlock(b *blocks.AtomicBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(&b.CommonBlock); err != nil {
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
		StateVersions: v,
		Tx:            b.Tx,
	}

	if err := b.Tx.Unsigned.Visit(atomicExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(b.Tx, status.Committed)

	if err := v.verifyUniqueInputs(b, atomicExecutor.Inputs); err != nil {
		return err
	}

	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,
		onAcceptState:  atomicExecutor.OnAccept,
		standardBlockState: standardBlockState{
			inputs: atomicExecutor.Inputs,
		},
		atomicRequests: atomicExecutor.AtomicRequests,
		timestamp:      atomicExecutor.OnAccept.GetTimestamp(),
	}

	v.Mempool.RemoveDecisionTxs([]*txs.Tx{b.Tx})
	return nil
}

func (v *verifier) StandardBlock(b *blocks.StandardBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}
	blkState := &blockState{
		statelessBlock: b,
		atomicRequests: make(map[ids.ID]*atomic.Requests),
	}

	if err := v.verifyCommonBlock(&b.CommonBlock); err != nil {
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

func (v *verifier) CommitBlock(b *blocks.CommitBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(&b.CommonBlock); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}

	onAcceptState := parentState.onCommitState
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,
		timestamp:      onAcceptState.GetTimestamp(),
		onAcceptState:  onAcceptState,
	}
	return nil
}

func (v *verifier) AbortBlock(b *blocks.AbortBlock) error {
	blkID := b.ID()

	if _, ok := v.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	if err := v.verifyCommonBlock(&b.CommonBlock); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("could not retrieve state for %s, parent of %s", parentID, blkID)
	}

	onAcceptState := parentState.onAbortState
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,
		timestamp:      onAcceptState.GetTimestamp(),
		onAcceptState:  onAcceptState,
	}
	return nil
}

func (v *verifier) verifyCommonBlock(b *blocks.CommonBlock) error {
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
