// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"go.uber.org/zap"
)

var _ stateless.Visitor = &acceptor{}

// acceptor handles the logic for accepting a block.
type acceptor struct {
	*backend
	metrics          metrics.Metrics
	recentlyAccepted *window.Window
}

func (a *acceptor) ProposalBlock(b *stateless.ProposalBlock) error {
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
	a.ctx.Log.Verbo(
		"accepting Proposal Block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	// See comment for [lastAccepted].
	a.backend.lastAccepted = blkID
	return nil
}

func (a *acceptor) AtomicBlock(b *stateless.AtomicBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo(
		"accepting Atomic Block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	// Update the state to reflect the changes made in [onAcceptState].
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

	// Note that this method writes [batch] to the database.
	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.Tx.ID(),
			blkID,
			err,
		)
	}
	return nil
}

func (a *acceptor) StandardBlock(b *stateless.StandardBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	a.ctx.Log.Verbo(
		"accepting Standard Block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parent", b.Parent()),
	)

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s", blkID)
	}

	// Update the state to reflect the changes made in [onAcceptState].
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

	// Note that this method writes [batch] to the database.
	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}
	return nil
}

func (a *acceptor) CommitBlock(b *stateless.CommitBlock) error {
	return a.acceptOptionBlock(b, true /* isCommit */)
}

func (a *acceptor) AbortBlock(b *stateless.AbortBlock) error {
	return a.acceptOptionBlock(b, false /* isCommit */)
}

func (a *acceptor) acceptOptionBlock(b stateless.Block, isCommit bool) error {
	blkID := b.ID()
	defer a.free(blkID)

	parentID := b.Parent()
	// Note: we assume this block's sibling doesn't
	// need the parent's state when it's rejected.
	defer a.free(parentID)

	if isCommit {
		a.ctx.Log.Verbo(
			"accepting Commit Block",
			zap.Stringer("blkID", blkID),
			zap.Uint64("height", b.Height()),
			zap.Stringer("parent", b.Parent()),
		)
	} else {
		a.ctx.Log.Verbo(
			"accepting Abort Block",
			zap.Stringer("blkID", blkID),
			zap.Uint64("height", b.Height()),
			zap.Stringer("parent", b.Parent()),
		)
	}

	parentState, ok := a.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("couldn't find state of block %s, parent of %s", parentID, blkID)
	}
	// Note that the parent must be accepted first.
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

	// Update the state to reflect the changes made in [onAcceptState].
	blkState.onAcceptState.Apply(a.state)
	return a.state.Commit()
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
	a.stateVersions.DeleteState(b.Parent())
	a.stateVersions.SetState(blkID, a.state)

	a.recentlyAccepted.Add(blkID)
	return nil
}
