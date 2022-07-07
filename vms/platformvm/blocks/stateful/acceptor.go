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

type acceptor struct {
	backend
	metrics          metrics.Metrics
	recentlyAccepted *window.Window
}

func (a *acceptor) VisitProposalBlock(b *stateless.ProposalBlock) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}
	a.ctx.Log.Verbo(
		"Accepting Proposal Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept proposal block %s: %w", b.ID(), err)
	}

	// Note that we do not write this block to state here.
	// (Namely, we don't call [a.commonAccept].)
	// That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	// We do this so that in the event that the node shuts down, the proposal block
	// is not written to disk unless its child is.
	// (The VM's Shutdown method commits the database.)
	// There is an invariant that the most recently committed block is a decision block.

	// TODO remove
	// b.status = choices.Accepted

	// a.blkIDToStatus[blkID] = choices.Accepted
	blkState.status = choices.Accepted
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}
	a.state.SetLastAccepted(blkID, false /*persist*/)
	return nil
}

func (a *acceptor) VisitAtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	defer a.backend.free(blkID)

	a.ctx.Log.Verbo(
		"Accepting Atomic Block %s at height %d with parent %s",
		blkID,
		b.Height(),
		b.Parent(),
	)

	a.commonAccept(blkState, b)
	a.AddStatelessBlock(b, choices.Accepted)
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept atomic block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	// if onAcceptState := a.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
	// 	onAcceptState.Apply(a.getState())
	// }
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

	// if err := a.ctx.SharedMemory.Apply(a.blkIDToAtomicRequests[blkID], batch); err != nil {
	// 	return fmt.Errorf(
	// 		"failed to atomically accept tx %s in block %s: %w",
	// 		b.Tx.ID(),
	// 		blkID,
	// 		err,
	// 	)
	// }
	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.Tx.ID(),
			blkID,
			err,
		)
	}

	// for _, child := range a.blkIDToChildren[blkID] {
	// 	child.setBaseState()
	// }
	for _, childID := range blkState.children {
		childState, ok := a.blkIDToState[childID]
		if !ok {
			return fmt.Errorf("couldn't find state of block %s, child of %s", childID, blkID)
		}
		_ = childState
		// TODO
		// childState.setBaseState()
	}

	// if onAcceptFunc := a.blkIDToOnAcceptFunc[blkID]; onAcceptFunc != nil {
	// 	onAcceptFunc()
	// }
	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}

	return nil
}

func (a *acceptor) VisitStandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}
	defer a.free(blkID)

	a.ctx.Log.Verbo("accepting block with ID %s", blkID)

	a.commonAccept(blkState, b)
	a.AddStatelessBlock(b, choices.Accepted)
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept standard block %s: %w", blkID, err)
	}

	// Update the state of the chain in the database
	// if onAcceptState := a.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
	// 	onAcceptState.Apply(a.getState())
	// }
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

	// if err := a.ctx.SharedMemory.Apply(a.blkIDToAtomicRequests[blkID], batch); err != nil {
	// 	return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	// }
	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	// for _, child := range a.blkIDToChildren[blkID] {
	// 	child.setBaseState()
	// }
	// if onAcceptFunc := a.blkIDToOnAcceptFunc[blkID]; onAcceptFunc != nil {
	// 	onAcceptFunc()
	// }
	for _, childID := range blkState.children {
		childState, ok := a.blkIDToState[childID]
		if !ok {
			return fmt.Errorf("couldn't find state of block %s, child of %s", childID, blkID)
		}
		_ = childState
		// TODO
		// child.setBaseState()
	}
	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}
	return nil
}

func (a *acceptor) VisitCommitBlock(b *stateless.CommitBlock) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}
	defer a.free(blkID)

	a.ctx.Log.Verbo("Accepting block with ID %s", blkID)

	// parentIntf, err := a.parent(b.baseBlk)
	// if err != nil {
	// 	return err
	// }
	// parent, ok := parentIntf.(*ProposalBlock)
	// if !ok {
	// 	a.ctx.Log.Error("double decision block should only follow a proposal block")
	// 	return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	// }
	parentID := b.Parent()
	parentState := a.blkIDToState[parentID]
	parent, _, err := a.GetStatelessBlock(parentID)
	if err != nil {
		return err
	}
	defer a.free(parent.ID())

	a.commonAccept(parentState, parent)
	a.AddStatelessBlock(parent, choices.Accepted)

	a.commonAccept(blkState, b)
	a.AddStatelessBlock(b, choices.Accepted)

	// wasPreferred := a.blkIDToPreferCommit[blkID]
	wasPreferred := blkState.inititallyPreferCommit

	// Update metrics
	if a.bootstrapped.GetValue() {
		if wasPreferred {
			a.metrics.MarkAcceptedOptionVote()
		} else {
			a.metrics.MarkRejectedOptionVote()
		}
	}
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept commit option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(b.CommonBlock)
}

func (a *acceptor) VisitAbortBlock(b *stateless.AbortBlock) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}
	defer a.free(blkID)

	a.ctx.Log.Verbo("Accepting block with ID %s", blkID)

	parentID := b.Parent()
	parentState := a.blkIDToState[parentID]
	parent, _, err := a.GetStatelessBlock(parentID)
	if err != nil {
		return err
	}
	defer a.free(parentID)

	a.commonAccept(parentState, parent) // TODO pass in parent state
	a.AddStatelessBlock(parent, choices.Accepted)

	a.commonAccept(blkState, b)
	a.AddStatelessBlock(b, choices.Accepted)

	// Update metrics
	// wasPreferred := a.blkIDToPreferCommit[blkID]

	// TODO should this be the parent's state?
	wasPreferred := blkState.inititallyPreferCommit
	if a.bootstrapped.GetValue() {
		if wasPreferred {
			a.metrics.MarkAcceptedOptionVote()
		} else {
			a.metrics.MarkRejectedOptionVote()
		}
	}
	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept abort option block %s: %w", b.ID(), err)
	}

	return a.updateStateOptionBlock(b.CommonBlock)
}

// [b] must be embedded in a Commit or Abort block.
func (a *acceptor) updateStateOptionBlock(b stateless.CommonBlock) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	// Update the state of the chain in the database
	// if onAcceptState := a.blkIDToOnAcceptState[blkID]; onAcceptState != nil {
	// 	onAcceptState.Apply(a.getState())
	// }
	blkState.onAcceptState.Apply(a.getState())
	if err := a.state.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}

	for _, childID := range blkState.children {
		childState, ok := a.blkIDToState[childID]
		if !ok {
			return fmt.Errorf("couldn't find state of block %s, child of %s", childID, blkID)
		}
		_ = childState
		// TODO
		// childID.setBaseState()
	}
	// if onAcceptFunc := a.blkIDToOnAcceptFunc[blkID]; onAcceptFunc != nil {
	// 	onAcceptFunc()
	// }
	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}
	return nil
}

func (a *acceptor) commonAccept(blkState *blockState, b stateless.Block) {
	blkID := b.ID()
	// a.blkIDToStatus[blkID] = choices.Accepted
	blkState.status = choices.Accepted
	a.state.SetLastAccepted(blkID, true /*persist*/)
	a.SetHeight(b.Height())
	a.recentlyAccepted.Add(blkID)
}
