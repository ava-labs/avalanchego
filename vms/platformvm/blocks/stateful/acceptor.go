// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
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
	metrics          *metrics.Metrics
	recentlyAccepted *window.Window
	backend
}

func (a *acceptorImpl) acceptProposalBlock(b *ProposalBlock) error {
	blkID := b.ID()
	a.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	// Update the state of the chain in the database
	// apply baseOptionState first
	if b.Version() == stateless.BlueberryVersion {
		b.onBlueberryBaseOptionsState.Apply(a.state)
	}

	// Note that we do not write this block to state here.
	// (Namely, we don't call [a.commonAccept].)
	// That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	// We do this so that in the event that the node shuts down, the proposal block
	// is not written to disk unless its child is.
	// (The VM's Shutdown method commits the database.)
	// There is an invariant that the most recently committed block is a decision block.
	b.status = choices.Accepted
	if err := a.metrics.MarkAccepted(b.ProposalBlockIntf); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}
	a.SetLastAccepted(blkID, false /*persist*/)
	return nil
}

func (a *acceptorImpl) acceptAtomicBlock(b *AtomicBlock) error {
	defer b.free()

	blkID := b.ID()

	a.ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.AtomicBlockIntf, choices.Accepted)
	if err := a.metrics.MarkAccepted(b.AtomicBlockIntf); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	b.onAcceptState.Apply(a.getState())

	defer a.Abort()
	batch, err := a.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := a.ctx.SharedMemory.Apply(b.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.AtomicTx().ID(),
			blkID,
			err,
		)
	}

	for _, child := range b.children {
		child.setBaseState()
	}
	if b.onAcceptFunc != nil {
		b.onAcceptFunc()
	}

	return nil
}

func (a *acceptorImpl) acceptStandardBlock(b *StandardBlock) error {
	defer b.free()

	blkID := b.ID()
	a.ctx.Log.Verbo("accepting block with ID %s", blkID)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.StandardBlockIntf, choices.Accepted)
	if err := a.metrics.MarkAccepted(b.StandardBlockIntf); err != nil {
		return fmt.Errorf("failed to accept standard block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	b.onAcceptState.Apply(a.getState())

	defer a.Abort()
	batch, err := a.CommitBatch()
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

	for _, child := range b.children {
		child.setBaseState()
	}
	if b.onAcceptFunc != nil {
		b.onAcceptFunc()
	}

	return nil
}

func (a *acceptorImpl) acceptCommitBlock(b *CommitBlock) error {
	defer b.free()

	blkID := b.baseBlk.ID()
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
	defer parent.free()

	a.commonAccept(parent.commonBlock)
	a.AddStatelessBlock(parent.ProposalBlockIntf, choices.Accepted)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.OptionBlock, choices.Accepted)

	// Update metrics
	if a.bootstrapped.GetValue() {
		if b.wasPreferred {
			a.metrics.MarkAcceptedOptionVote()
		} else {
			a.metrics.MarkRejectedOptionVote()
		}
	}
	if err := a.metrics.MarkAccepted(b.OptionBlock); err != nil {
		return fmt.Errorf("failed to accept commit option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(b.decisionBlock)
}

func (a *acceptorImpl) acceptAbortBlock(b *AbortBlock) error {
	defer b.free()

	blkID := b.baseBlk.ID()
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
	defer parent.free()

	a.commonAccept(parent.commonBlock)
	a.AddStatelessBlock(parent.ProposalBlockIntf, choices.Accepted)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.OptionBlock, choices.Accepted)

	// Update metrics
	if a.bootstrapped.GetValue() {
		if b.wasPreferred {
			a.metrics.MarkAcceptedOptionVote()
		} else {
			a.metrics.MarkRejectedOptionVote()
		}
	}
	if err := a.metrics.MarkAccepted(b.OptionBlock); err != nil {
		return fmt.Errorf("failed to accept abort option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(b.decisionBlock)
}

// [b] must be embedded in a Commit or Abort block.
func (a *acceptorImpl) updateStateOptionBlock(b *decisionBlock) error {
	// Update the state of the chain in the database
	b.onAcceptState.Apply(a.getState())
	if err := a.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}

	for _, child := range b.children {
		child.setBaseState()
	}
	if b.onAcceptFunc != nil {
		b.onAcceptFunc()
	}
	return nil
}

func (a *acceptorImpl) commonAccept(b *commonBlock) {
	blkID := b.baseBlk.ID()
	b.status = choices.Accepted
	a.SetLastAccepted(blkID, true /*persist*/)
	a.SetHeight(b.baseBlk.Height())
	a.recentlyAccepted.Add(blkID)
}
