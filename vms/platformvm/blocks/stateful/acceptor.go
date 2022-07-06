// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
)

var _ acceptor = &acceptorImpl{}

type acceptor interface {
	acceptProposalBlock(b *ProposalBlock) error
	acceptAtomicBlock(b *AtomicBlock) error
	acceptStandardBlock(b *StandardBlock) error
	acceptCommitBlock(b *CommitBlock) error
	acceptAbortBlock(b *AbortBlock) error
}

type acceptorImpl struct {
	backend
	metrics          metrics.Metrics
	recentlyAccepted *window.Window
}

func (a *acceptorImpl) acceptProposalBlock(b *ProposalBlock) error {
	blkID := b.ID()
	a.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	if err := a.metrics.MarkAccepted(b.ProposalBlock); err != nil {
		return fmt.Errorf("failed to accept proposal block %s: %w", b.ID(), err)
	}

	// Note that we do not write this block to state here.
	// (Namely, we don't call [a.commonAccept].)
	// That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	// We do this so that in the event that the node shuts down, the proposal block
	// is not written to disk unless its child is.
	// (The VM's Shutdown method commits the database.)
	// There is an invariant that the most recently committed block is a decision block.
	b.status = choices.Accepted
	if err := a.metrics.MarkAccepted(b.ProposalBlock); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}
	a.state.SetLastAccepted(blkID, false /*persist*/)
	return nil
}

func (a *acceptorImpl) acceptAtomicBlock(b *AtomicBlock) error {
	blkID := b.ID()
	defer a.backend.free(blkID)

	a.ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.AtomicBlock, choices.Accepted)
	if err := a.metrics.MarkAccepted(b.AtomicBlock); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	if onAcceptState := a.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
		onAcceptState.Apply(a.getState())
	}

	defer a.state.Abort()
	batch, err := a.state.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := a.ctx.SharedMemory.Apply(a.blkIDToAtomicRequests[blkID], batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.AtomicBlock.Tx.ID(),
			blkID,
			err,
		)
	}

	for _, child := range a.blkIDToChildren[blkID] {
		child.setBaseState()
	}
	if onAcceptFunc := a.blkIDToOnAcceptFunc[blkID]; onAcceptFunc != nil {
		onAcceptFunc()
	}

	return nil
}

func (a *acceptorImpl) acceptStandardBlock(b *StandardBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo("accepting block with ID %s", blkID)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.StandardBlock, choices.Accepted)
	if err := a.metrics.MarkAccepted(b.StandardBlock); err != nil {
		return fmt.Errorf("failed to accept standard block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	if onAcceptState := a.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
		onAcceptState.Apply(a.getState())
	}

	defer a.state.Abort()
	batch, err := a.state.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := a.ctx.SharedMemory.Apply(b.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	for _, child := range a.blkIDToChildren[blkID] {
		child.setBaseState()
	}
	if onAcceptFunc := a.blkIDToOnAcceptFunc[blkID]; onAcceptFunc != nil {
		onAcceptFunc()
	}

	return nil
}

func (a *acceptorImpl) acceptCommitBlock(b *CommitBlock) error {
	blkID := b.baseBlk.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo("Accepting block with ID %s", blkID)

	parentIntf, err := a.parent(b.baseBlk)
	if err != nil {
		return err
	}
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		a.ctx.Log.Error("double decision block should only follow a proposal block")
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}
	defer a.free(parent.ID())

	a.commonAccept(parent.commonBlock)
	a.AddStatelessBlock(parent.ProposalBlock, choices.Accepted)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.CommitBlock, choices.Accepted)

	wasPreferred := a.blkIDToPreferCommit[blkID]

	// Update metrics
	if a.bootstrapped.GetValue() {
		if wasPreferred {
			a.metrics.MarkAcceptedOptionVote()
		} else {
			a.metrics.MarkRejectedOptionVote()
		}
	}
	if err := a.metrics.MarkAccepted(b.CommitBlock); err != nil {
		return fmt.Errorf("failed to accept commit option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(b.commonBlock)
}

func (a *acceptorImpl) acceptAbortBlock(b *AbortBlock) error {
	blkID := b.baseBlk.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo("Accepting block with ID %s", blkID)

	parentIntf, err := a.parent(b.baseBlk)
	if err != nil {
		return err
	}
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		a.ctx.Log.Error("double decision block should only follow a proposal block")
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}
	defer a.free(parent.ID())

	a.commonAccept(parent.commonBlock)
	a.AddStatelessBlock(parent.ProposalBlock, choices.Accepted)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.AbortBlock, choices.Accepted)

	// Update metrics
	wasPreferred := a.blkIDToPreferCommit[blkID]
	if a.bootstrapped.GetValue() {
		if wasPreferred {
			a.metrics.MarkAcceptedOptionVote()
		} else {
			a.metrics.MarkRejectedOptionVote()
		}
	}
	if err := a.metrics.MarkAccepted(b.AbortBlock); err != nil {
		return fmt.Errorf("failed to accept abort option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(b.commonBlock)
}

// [b] must be embedded in a Commit or Abort block.
func (a *acceptorImpl) updateStateOptionBlock(b *commonBlock) error {
	blkID := b.baseBlk.ID()
	// Update the state of the chain in the database
	if onAcceptState := a.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
		onAcceptState.Apply(a.getState())
	}
	if err := a.state.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}

	for _, child := range a.blkIDToChildren[blkID] {
		child.setBaseState()
	}
	if onAcceptFunc := a.blkIDToOnAcceptFunc[blkID]; onAcceptFunc != nil {
		onAcceptFunc()
	}
	return nil
}

func (a *acceptorImpl) commonAccept(b *commonBlock) {
	blkID := b.baseBlk.ID()
	b.status = choices.Accepted
	a.state.SetLastAccepted(blkID, true /*persist*/)
	a.SetHeight(b.baseBlk.Height())
	a.recentlyAccepted.Add(blkID)
}
