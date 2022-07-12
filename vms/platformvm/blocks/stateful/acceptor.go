// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
)

var _ stateless.Visitor = &acceptor{}

type acceptor struct {
	backend
	metrics          metrics.Metrics
	recentlyAccepted *window.Window
}

func (a *acceptor) VisitProposalBlock(b *stateless.ProposalBlock) error {
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

	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept proposal block %s: %w", b.ID(), err)
	}

	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}

	// Note that we do not write this block to state here.
	// That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	// We do this so that in the event that the node shuts down, the proposal block
	// is not written to disk unless its child is.
	// (The VM's Shutdown method commits the database.)
	// There is an invariant that the most recently committed block is a decision block.
	a.state.SetLastAccepted(blkID, false /*persist*/)
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

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	a.commonAccept(b)
	a.AddStatelessBlock(b, choices.Accepted)
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}

	blkState.onAcceptState.Apply(a.getState())

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

	for _, childID := range blkState.children {
		childState, ok := a.blkIDToState[childID]
		if !ok {
			return fmt.Errorf("couldn't find state of block %s, child of %s", childID, blkID)
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

	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}

	return nil
}

func (a *acceptor) VisitStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo("accepting block with ID %s", blkID)

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	a.commonAccept(b)
	a.AddStatelessBlock(b, choices.Accepted)
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept standard block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	blkState.onAcceptState.Apply(a.getState())

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

	for _, childID := range blkState.children {
		childState, ok := a.blkIDToState[childID]
		if !ok {
			return fmt.Errorf("couldn't find state of block %s, child of %s", childID, blkID)
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
	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}
	return nil
}

func (a *acceptor) VisitCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	parentID := b.Parent()
	defer a.free(parentID)

	a.ctx.Log.Verbo("accepting block %s", blkID)

	parentState, ok := a.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s, parent of %s", parentID, blkID)
	}
	a.commonAccept(parentState.statelessBlock)
	a.AddStatelessBlock(parentState.statelessBlock, choices.Accepted)

	a.commonAccept(b)
	a.AddStatelessBlock(b, choices.Accepted)

	// Update metrics
	wasPreferred := parentState.inititallyPreferCommit
	if a.bootstrapped.GetValue() {
		if wasPreferred {
			a.metrics.MarkVoteWon()
		} else {
			a.metrics.MarkVoteLost()
		}
	}
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept commit option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(blkID)
}

func (a *acceptor) VisitAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	parentID := b.Parent()
	defer a.free(parentID)

	a.ctx.Log.Verbo("Accepting block with ID %s", blkID)

	parentState := a.blkIDToState[parentID]
	a.commonAccept(parentState.statelessBlock)
	a.AddStatelessBlock(parentState.statelessBlock, choices.Accepted)

	a.commonAccept(b)
	a.AddStatelessBlock(b, choices.Accepted)

	// Update metrics
	wasPreferred := parentState.inititallyPreferCommit
	if a.bootstrapped.GetValue() {
		if wasPreferred {
			a.metrics.MarkVoteWon()
		} else {
			a.metrics.MarkVoteLost()
		}
	}
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept abort option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(blkID)
}

// [b] must be embedded in a Commit or Abort block.
func (a *acceptor) updateStateOptionBlock(blkID ids.ID) error {
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	// Update the state of the chain in the database
	blkState.onAcceptState.Apply(a.getState())
	if err := a.state.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}

	for _, childID := range blkState.children {
		childState, ok := a.blkIDToState[childID]
		if !ok {
			return fmt.Errorf("couldn't find state of block %s, child of %s", childID, blkID)
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
	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}
	return nil
}

func (a *acceptor) commonAccept(b stateless.Block) {
	blkID := b.ID()
	a.state.SetLastAccepted(blkID, true /*persist*/)
	a.SetHeight(b.Height())
	a.recentlyAccepted.Add(blkID)
}
