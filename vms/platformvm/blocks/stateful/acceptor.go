// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
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
}

func (a *acceptorImpl) acceptProposalBlock(b *ProposalBlock) error {
	blkID := b.ID()
	a.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	// TODO should the line below be here?
	a.commonAccept(b.commonBlock)
	b.status = choices.Accepted
	a.SetLastAccepted(blkID)
	return nil
}

func (a *acceptorImpl) acceptAtomicBlock(b *AtomicBlock) error {
	blkID := b.ID()

	a.ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.AtomicBlock, b.Status())
	if err := a.MarkAccepted(b.AtomicBlock); err != nil {
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

	if err = a.ctx.SharedMemory.Apply(b.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.AtomicBlock.Tx.ID(),
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

	b.free()
	return nil
}

func (a *acceptorImpl) acceptStandardBlock(b *StandardBlock) error {
	blkID := b.ID()
	a.ctx.Log.Verbo("accepting block with ID %s", blkID)

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.StandardBlock, b.Status())
	if err := a.MarkAccepted(b.StandardBlock); err != nil {
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

	b.free()
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
	a.AddStatelessBlock(parent.ProposalBlock, parent.Status())

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.CommitBlock, b.Status())

	// Update metrics
	if a.bootstrapped.GetValue() {
		if b.wasPreferred {
			a.MarkAcceptedOptionVote()
		} else {
			a.MarkRejectedOptionVote()
		}
	}
	if err := a.MarkAccepted(b.CommitBlock); err != nil {
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
	a.AddStatelessBlock(parent.ProposalBlock, parent.Status())

	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.AbortBlock, b.Status())

	// Update metrics
	if a.bootstrapped.GetValue() {
		if b.wasPreferred {
			a.MarkAcceptedOptionVote()
		} else {
			a.MarkRejectedOptionVote()
		}
	}
	if err := a.MarkAccepted(b.AbortBlock); err != nil {
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

// TODO is this right?
func (a *acceptorImpl) commonAccept(b *commonBlock) {
	blkID := b.baseBlk.ID()
	b.status = choices.Accepted
	a.SetLastAccepted(blkID)
	a.SetHeight(b.baseBlk.Height())
	// TODO do we need the line below?
	// a.addToRecentlyAcceptedWindows(blkID)
}

/* TODO do we need this?
func (a *acceptorImpl) addToRecentlyAcceptedWindows(blkID ids.ID) {}
*/
