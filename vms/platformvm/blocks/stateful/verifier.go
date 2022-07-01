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
	verifyProposalBlock(b *ProposalBlock) error
	verifyAtomicBlock(b *AtomicBlock) error
	verifyStandardBlock(b *StandardBlock) error
	verifyCommitBlock(b *CommitBlock) error
	verifyAbortBlock(b *AbortBlock) error
	verifyCommonBlock(b *commonBlock) error
}

func NewVerifier() verifier {
	// TODO implement
	return &verifierImpl{}
}

type verifierImpl struct {
	backend
}

func (v *verifierImpl) verifyProposalBlock(b *ProposalBlock) error {
	if err := b.verify(); err != nil {
		return err
	}

	parentIntf, parentErr := v.parent(b.baseBlk)
	if parentErr != nil {
		return parentErr
	}

	/* TODO remove
	// The parent of a proposal block (ie this block) must be a decision block
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}
	*/

	// parentState is the state if this block's parent is accepted
	parentState := v.onAccept(parentIntf)

	txExecutor := executor.ProposalTxExecutor{
		Backend:     &b.txExecutorBackend,
		ParentState: parentState,
		Tx:          b.Tx,
	}
	err := b.Tx.Unsigned.Visit(&txExecutor)
	if err != nil {
		txID := b.Tx.ID()
		v.markDropped(txID, err.Error()) // cache tx as dropped
		return err
	}

	b.onCommitState = txExecutor.OnCommit
	b.onAbortState = txExecutor.OnAbort
	b.prefersCommit = txExecutor.PrefersCommit

	b.onCommitState.AddTx(b.Tx, status.Committed)
	b.onAbortState.AddTx(b.Tx, status.Aborted)

	b.timestamp = parentState.GetTimestamp()

	v.removeProposalTx(b.Tx)
	v.cacheVerifiedBlock(b)
	parentIntf.addChild(b)
	return nil
}

func (v *verifierImpl) verifyAtomicBlock(b *AtomicBlock) error {
	if err := b.verify(); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}

	/* TODO remove
	// AtomicBlock is not a modifier on a proposal block, so its parent must be
	// a decision.
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}
	*/

	parentState := v.onAccept(parentIntf)

	cfg := b.txExecutorBackend.Cfg
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
		Backend:     &b.txExecutorBackend,
		ParentState: parentState,
		Tx:          b.Tx,
	}
	err = b.Tx.Unsigned.Visit(&atomicExecutor)
	if err != nil {
		txID := b.Tx.ID()
		v.markDropped(txID, err.Error()) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(b.Tx, status.Committed)

	b.onAcceptState = atomicExecutor.OnAccept
	b.inputs = atomicExecutor.Inputs
	b.atomicRequests = atomicExecutor.AtomicRequests
	b.timestamp = atomicExecutor.OnAccept.GetTimestamp()

	conflicts, err := parentIntf.conflicts(b.inputs)
	if err != nil {
		return err
	}
	if conflicts {
		return ErrConflictingParentTxs
	}

	v.removeDecisionTxs([]*txs.Tx{b.Tx})
	v.cacheVerifiedBlock(b)
	parentIntf.addChild(b)
	return nil
}

func (v *verifierImpl) verifyStandardBlock(b *StandardBlock) error {
	if err := b.verify(); err != nil {
		return err
	}

	parentIntf, err := v.parent(b.baseBlk)
	if err != nil {
		return err
	}

	/* TODO remove
	// StandardBlock is not a modifier on a proposal block, so its parent must
	// be a decision.
	parent, ok := parentIntf.(Decision)
	if !ok {
		return fmt.Errorf("expected Decision block but got %T", parentIntf)
	}
	*/

	parentState := v.onAccept(parentIntf)
	b.onAcceptState = state.NewDiff(
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
			Backend: &b.txExecutorBackend,
			State:   b.onAcceptState,
			Tx:      tx,
		}
		err := tx.Unsigned.Visit(&txExecutor)
		if err != nil {
			txID := tx.ID()
			v.markDropped(txID, err.Error()) // cache tx as dropped
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

	b.timestamp = b.onAcceptState.GetTimestamp()

	v.removeDecisionTxs(b.Txs)
	v.cacheVerifiedBlock(b)
	parentIntf.addChild(b)
	return nil
}

func (v *verifierImpl) verifyCommitBlock(b *CommitBlock) error {
	if err := b.verify(); err != nil {
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
	b.timestamp = b.onAcceptState.GetTimestamp()

	v.cacheVerifiedBlock(b)
	parent.addChild(b)
	return nil
}

func (v *verifierImpl) verifyAbortBlock(b *AbortBlock) error {
	if err := b.verify(); err != nil {
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
	b.timestamp = b.onAcceptState.GetTimestamp()

	v.cacheVerifiedBlock(b)
	parent.addChild(b)
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

/*
type Verifier interface {
	mempool.Mempool
	stateless.Metrics

	state.State
	SetHeight(height uint64)

	AddStatelessBlock(block stateless.Block, status choices.Status)
	GetState() state.State
	GetChainState() state.Chain
	Abort()
	Commit() error
	CommitBatch() (database.Batch, error)

	GetStatefulBlock(blkID ids.ID) (Block, error)
	CacheVerifiedBlock(Block)
	DropVerifiedBlock(blkID ids.ID)

	// register recently accepted blocks, needed
	// to calculate the minimum height of the block still in the
	// Snowman++ proposal window.
	AddToRecentlyAcceptedWindows(blkID ids.ID)
}
*/
