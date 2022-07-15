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

var _ stateless.Visitor = &acceptor{}

// acceptor handles the logic for accepting a block.
type acceptor struct {
	*backend
	metrics          metrics.Metrics
	recentlyAccepted *window.Window
}

func (a *acceptor) VisitBlueberryProposalBlock(b *stateless.BlueberryProposalBlock) error {
	/* Note that:

	* We don't free the proposal block in this method.
	  It is freed when its child is accepted.
	  We need to keep this block's state in memory for its child to use.

	* We only update the metrics to reflect this block's
	  acceptance when its child is accepted.

	* We don't write this block to state here.
	  That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	  We do this so that in the event that the node shuts down, the proposal block
	  is not written to disk unless its child is.
	  (The VM's Shutdown method commits the database.)
	  The snowman.Engine requires that the last committed block is a decision block.

	*/

	blkID := b.ID()
	// Note that we don't free the proposal block here.
	// It is freed when its child is accepted.
	// We need to keep this block's state in memory for its child to use.

	a.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	// Update the state of the chain in the database
	// apply baseOptionState first
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}
	blkState.onBlueberryBaseOptionsState.Apply(a.state)

	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept proposal block %s: %w", b.ID(), err)
	}

	// Note that we do not write this block to state here.
	// That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	// We do this so that in the event that the node shuts down, the proposal block
	// is not written to disk unless its child is.
	// (The VM's Shutdown method commits the database.)
	// The snowman.Engine requires that the last committed block is a decision block.
	a.backend.lastAccepted = blkID
	return nil
}

func (a *acceptor) VisitApricotProposalBlock(b *stateless.ApricotProposalBlock) error {
	blkID := b.ID()

	a.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	// See comment for [lastAccepted].
	a.backend.lastAccepted = blkID
	return nil
}

func (a *acceptor) VisitAtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	// Update the state of the chain in the database
	blkState.onAcceptState.Apply(a.state)

	defer a.state.Abort()
	batch, err := a.state.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.Tx.ID(),
			blkID,
			err,
		)
	}
	return a.updateChildrenState(blkState)
}

func (a *acceptor) VisitBlueberryStandardBlock(b *stateless.BlueberryStandardBlock) error {
	return a.visitStandardBlock(b)
}

func (a *acceptor) VisitApricotStandardBlock(b *stateless.ApricotStandardBlock) error {
	return a.visitStandardBlock(b)
}

func (a *acceptor) visitStandardBlock(b stateless.Block) error {
	blkID := b.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo("accepting block with ID %s", blkID)

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	// Update the state of the chain in the database
	blkState.onAcceptState.Apply(a.state)

	defer a.state.Abort()
	batch, err := a.state.CommitBatch()
	if err != nil {
		return fmt.Errorf(
			"failed to commit VM's database for block %s: %w",
			blkID,
			err,
		)
	}

	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	if err := a.updateChildrenState(blkState); err != nil {
		return err
	}

	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}
	return nil
}

func (a *acceptor) VisitCommitBlock(b *stateless.CommitBlock) error {
	return a.acceptOptionBlock(b)
}

func (a *acceptor) VisitAbortBlock(b *stateless.AbortBlock) error {
	return a.acceptOptionBlock(b)
}

func (a *acceptor) acceptOptionBlock(b stateless.Block) error {
	blkID := b.ID()
	defer a.free(blkID)

	parentID := b.Parent()
	// Note: we assume this block's sibling doesn't
	// need the parent's state when it's rejected.
	defer a.free(parentID)

	a.ctx.Log.Verbo("accepting block %s", blkID)

	parentState, ok := a.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s, parent of %s", parentID, blkID)
	}
	if err := a.commonAccept(parentState.statelessBlock); err != nil {
		return err
	}
	if err := a.commonAccept(b); err != nil {
		return err
	}

	// Update metrics
	if a.bootstrapped.GetValue() {
		wasPreferred := parentState.initiallyPreferCommit
		if wasPreferred {
			a.metrics.MarkVoteWon()
		} else {
			a.metrics.MarkVoteLost()
		}
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	// Update the state of the chain in the database
	blkState.onAcceptState.Apply(a.state)

	if err := a.state.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}
	return a.updateChildrenState(blkState)
}

// Update the state of the children of the block which is being accepted.
func (a *acceptor) updateChildrenState(blkState *blockState) error {
	for _, childID := range blkState.children {
		childState, ok := a.blkIDToState[childID]
		if !ok {
			return fmt.Errorf("couldn't find state of block %s, child of %s", childID, blkState.statelessBlock.ID())
		}
		if childState.onCommitState != nil {
			childState.onCommitState.SetBase(a.state)
		}
		if childState.onAbortState != nil {
			childState.onAbortState.SetBase(a.state)
		}
		if childState.onAcceptState != nil {
			childState.onAcceptState.Apply(a.state)
		}
	}
	return nil
}

func (a *acceptor) commonAccept(b stateless.Block) error {
	blkID := b.ID()
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept block %s: %w", blkID, err)
	}
	a.backend.lastAccepted = blkID
	a.state.SetLastAccepted(blkID)
	a.state.SetHeight(b.Height())
	a.state.AddStatelessBlock(b, choices.Accepted)
	a.recentlyAccepted.Add(blkID)
	return nil
}
