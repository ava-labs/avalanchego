// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
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
	if err := v.verifyCommonBlock(b.commonBlock); err != nil {
		return err
	}

	parentState := v.OnAccept(b.Parent())

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

	blkID := b.ID()
	b.prefersCommit = txExecutor.PrefersCommit

	onCommitState := txExecutor.OnCommit
	onCommitState.AddTx(b.Tx, status.Committed)
	v.blkIDToOnCommitState[blkID] = onCommitState

	onAbortState := txExecutor.OnAbort
	onAbortState.AddTx(b.Tx, status.Aborted)
	v.blkIDToOnAbortState[blkID] = onAbortState

	v.blkIDToTimestamp[blkID] = parentState.GetTimestamp()

	v.Mempool.RemoveProposalTx(b.Tx)
	v.pinVerifiedBlock(b)
	parentID := b.Parent()
	v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

func (v *verifierImpl) verifyAtomicBlock(b *AtomicBlock) error {
	if err := v.verifyCommonBlock(b.commonBlock); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
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

	blkID := b.ID()
	v.blkIDToOnAcceptState[blkID] = atomicExecutor.OnAccept
	b.inputs = atomicExecutor.Inputs
	b.atomicRequests = atomicExecutor.AtomicRequests
	v.blkIDToTimestamp[blkID] = atomicExecutor.OnAccept.GetTimestamp()

	conflicts, err := parentIntf.conflicts(b.inputs)
	if err != nil {
		return err
	}
	if conflicts {
		return ErrConflictingParentTxs
	}

	v.Mempool.RemoveDecisionTxs([]*txs.Tx{b.Tx})
	v.pinVerifiedBlock(b)
	parentID := b.Parent()
	v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

func (v *verifierImpl) verifyStandardBlock(b *StandardBlock) error {
	blkID := b.ID()

	if err := v.verifyCommonBlock(b.commonBlock); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}

	parentState := v.OnAccept(b.Parent())

	onAcceptState := state.NewDiff(
		parentState,
		parentState.CurrentStakers(),
		parentState.PendingStakers(),
	)

	// clear inputs so that multiple [Verify] calls can be made
	b.Inputs.Clear()
	b.atomicRequests = make(map[ids.ID]*atomic.Requests)

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
		if b.Inputs.Overlaps(txExecutor.Inputs) {
			return errConflictingBatchTxs
		}
		// Add UTXOs to batch
		b.Inputs.Union(txExecutor.Inputs)

		onAcceptState.AddTx(tx, status.Committed)
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
		v.blkIDToOnAcceptFunc[blkID] = funcs[0]
	} else if numFuncs > 1 {
		v.blkIDToOnAcceptFunc[blkID] = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	v.blkIDToOnAcceptState[blkID] = onAcceptState
	v.Mempool.RemoveDecisionTxs(b.Txs)
	v.pinVerifiedBlock(b)
	parentID := b.Parent()
	v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

func (v *verifierImpl) verifyCommitBlock(b *CommitBlock) error {
	if err := v.verifyCommonBlock(b.commonBlock); err != nil {
		return err
	}

	onAcceptState := v.blkIDToOnCommitState[b.Parent()]
	blkID := b.ID()
	v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	v.blkIDToOnAcceptState[blkID] = onAcceptState

	v.pinVerifiedBlock(b)
	parentID := b.Parent()
	v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

func (v *verifierImpl) verifyAbortBlock(b *AbortBlock) error {
	if err := v.verifyCommonBlock(b.commonBlock); err != nil {
		return err
	}

	onAcceptState := v.blkIDToOnAbortState[b.Parent()]
	blkID := b.ID()
	v.blkIDToTimestamp[blkID] = onAcceptState.GetTimestamp()
	v.blkIDToOnAcceptState[blkID] = onAcceptState

	v.pinVerifiedBlock(b)
	parentID := b.Parent()
	v.blkIDToChildren[parentID] = append(v.blkIDToChildren[parentID], b)
	return nil
}

// Assumes [b] isn't nil
func (v *verifierImpl) verifyCommonBlock(b *commonBlock) error {
	parent, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}
	if expectedHeight := parent.Height() + 1; expectedHeight != b.baseBlk.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			b.baseBlk.Height(),
		)
	}
	return nil
}
