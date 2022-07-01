// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var _ acceptor = &acceptorImpl{}

type acceptor interface {
	acceptProposalBlock(b *ProposalBlock) error
	acceptAtomicBlock(b *AtomicBlock) error
	acceptStandardBlock(b *StandardBlock) error
	acceptCommitBlock(b *CommitBlock) error
	acceptAbortBlock(b *AbortBlock) error
	GetLastAccepted() ids.ID
}

func NewAcceptor() acceptor {
	// TODO implement
	return &acceptorImpl{}
}

type acceptorImpl struct {
	backend
}

func (a *acceptorImpl) acceptProposalBlock(b *ProposalBlock) error {
	blkID := b.ID()
	b.txExecutorBackend.Ctx.Log.Verbo(
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

	b.txExecutorBackend.Ctx.Log.Verbo(
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

	if err = b.txExecutorBackend.Ctx.SharedMemory.Apply(b.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.AtomicBlock.Tx.ID(),
			blkID,
			err,
		)
	}

	for _, child := range b.children {
		a.setBaseState(child)
	}
	if b.onAcceptFunc != nil {
		b.onAcceptFunc()
	}

	b.free()
	return nil
}

func (a *acceptorImpl) acceptStandardBlock(b *StandardBlock) error {
	blkID := b.ID()
	b.txExecutorBackend.Ctx.Log.Verbo("accepting block with ID %s", blkID)

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

	if err := b.txExecutorBackend.Ctx.SharedMemory.Apply(b.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	for _, child := range b.children {
		a.setBaseState(child)
	}
	if b.onAcceptFunc != nil {
		b.onAcceptFunc()
	}

	b.free()
	return nil
}

func (a *acceptorImpl) acceptCommitBlock(b *CommitBlock) error {
	if b.txExecutorBackend.Bootstrapped.GetValue() {
		if b.wasPreferred {
			a.MarkAcceptedOptionVote()
		} else {
			a.MarkRejectedOptionVote()
		}
	}

	if err := a.acceptParentDoubleDecisionBlock(b.doubleDecisionBlock); err != nil {
		return err
	}
	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.CommitBlock, b.Status())
	if err := a.MarkAccepted(b.CommitBlock); err != nil {
		return fmt.Errorf("failed to accept accept option block %s: %w", b.ID(), err)
	}

	defer b.free()
	return a.updateStateDoubleDecisionBlock(b.doubleDecisionBlock)
}

func (a *acceptorImpl) acceptAbortBlock(b *AbortBlock) error {
	if b.txExecutorBackend.Bootstrapped.GetValue() {
		if b.wasPreferred {
			a.MarkAcceptedOptionVote()
		} else {
			a.MarkRejectedOptionVote()
		}
	}

	if err := a.acceptParentDoubleDecisionBlock(b.doubleDecisionBlock); err != nil {
		return err
	}
	a.commonAccept(b.commonBlock)
	a.AddStatelessBlock(b.AbortBlock, b.Status())
	if err := a.MarkAccepted(b.AbortBlock); err != nil {
		return fmt.Errorf("failed to accept accept option block %s: %w", b.ID(), err)
	}

	defer b.free()
	return a.updateStateDoubleDecisionBlock(b.doubleDecisionBlock)
}

func (a *acceptorImpl) updateStateDoubleDecisionBlock(b *doubleDecisionBlock) error {
	parentIntf, err := a.parent(b.baseBlk)
	if err != nil {
		return err
	}

	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		b.txExecutorBackend.Ctx.Log.Error("double decision block should only follow a proposal block")
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	// Update the state of the chain in the database
	b.onAcceptState.Apply(a.getState())
	if err := a.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}

	for _, child := range b.children {
		a.setBaseState(child)
	}
	if b.onAcceptFunc != nil {
		b.onAcceptFunc()
	}

	// remove this block and its parent from memory
	parent.free()
	return nil
}

func (a *acceptorImpl) acceptParentDoubleDecisionBlock(b *doubleDecisionBlock) error {
	blkID := b.baseBlk.ID()
	b.txExecutorBackend.Ctx.Log.Verbo("Accepting block with ID %s", blkID)

	parentIntf, err := a.parent(b.baseBlk)
	if err != nil {
		return err
	}

	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		b.txExecutorBackend.Ctx.Log.Error("double decision block should only follow a proposal block")
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	if err := parent.Accept(); err != nil {
		return fmt.Errorf("failed to accept parent's CommonBlock: %w", err)
	}
	a.AddStatelessBlock(parent, parent.Status())

	return nil
}

// TODO document acceptable input types
func (a *acceptorImpl) setBaseState(b Block) {
	switch b := b.(type) {
	case *AbortBlock:
		b.onAcceptState.SetBase(a.getState())
	case *CommitBlock:
		b.onAcceptState.SetBase(a.getState())
	case *ProposalBlock:
		b.onCommitState.SetBase(a.getState())
		b.onAbortState.SetBase(a.getState())
	}
}

func (a *acceptorImpl) commonAccept(b *commonBlock) {
	blkID := b.baseBlk.ID()
	b.status = choices.Accepted
	a.SetLastAccepted(blkID)
	a.SetHeight(b.baseBlk.Height())
	// TODO do we need the line below?
	// a.addToRecentlyAcceptedWindows(blkID)
}

/* TODO
func (a *acceptorImpl) addToRecentlyAcceptedWindows(blkID ids.ID) {}
*/
