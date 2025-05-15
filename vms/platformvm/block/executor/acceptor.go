// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

var (
	_ block.Visitor = (*acceptor)(nil)

	errMissingBlockState = errors.New("missing state of block")
)

// acceptor handles the logic for accepting a block.
// All errors returned by this struct are fatal and should result in the chain
// being shutdown.
type acceptor struct {
	*backend
	metrics    metrics.Metrics
	validators validators.Manager
}

func (a *acceptor) BanffAbortBlock(b *block.BanffAbortBlock) error {
	return a.optionBlock(b, "banff abort")
}

func (a *acceptor) BanffCommitBlock(b *block.BanffCommitBlock) error {
	return a.optionBlock(b, "banff commit")
}

func (a *acceptor) BanffProposalBlock(b *block.BanffProposalBlock) error {
	a.proposalBlock(b, "banff proposal")
	return nil
}

func (a *acceptor) BanffStandardBlock(b *block.BanffStandardBlock) error {
	return a.standardBlock(b, "banff standard")
}

func (a *acceptor) ApricotAbortBlock(b *block.ApricotAbortBlock) error {
	return a.optionBlock(b, "apricot abort")
}

func (a *acceptor) ApricotCommitBlock(b *block.ApricotCommitBlock) error {
	return a.optionBlock(b, "apricot commit")
}

func (a *acceptor) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	a.proposalBlock(b, "apricot proposal")
	return nil
}

func (a *acceptor) ApricotStandardBlock(b *block.ApricotStandardBlock) error {
	return a.standardBlock(b, "apricot standard")
}

func (a *acceptor) ApricotAtomicBlock(b *block.ApricotAtomicBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
	}

	if err := a.commonAccept(blkState); err != nil {
		return err
	}

	// Update the state to reflect the changes made in [onAcceptState].
	if err := blkState.onAcceptState.Apply(a.state); err != nil {
		return err
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

	// Note that this method writes [batch] to the database.
	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf(
			"failed to atomically accept tx %s in block %s: %w",
			b.Tx.ID(),
			blkID,
			err,
		)
	}

	a.ctx.Log.Trace(
		"accepted block",
		zap.String("blockType", "apricot atomic"),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
		zap.Stringer("checksum", a.state.Checksum()),
	)

	return nil
}

func (a *acceptor) optionBlock(b block.Block, blockType string) error {
	parentID := b.Parent()
	parentState, ok := a.blkIDToState[parentID]
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkID := b.ID()
	defer func() {
		// Note: we assume this block's sibling doesn't
		// need the parent's state when it's rejected.
		a.free(parentID)
		a.free(blkID)
	}()

	// Note that the parent must be accepted first.
	if err := a.commonAccept(parentState); err != nil {
		return err
	}

	if parentState.onDecisionState != nil {
		if err := parentState.onDecisionState.Apply(a.state); err != nil {
			return err
		}
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
	}

	if err := a.commonAccept(blkState); err != nil {
		return err
	}

	if err := blkState.onAcceptState.Apply(a.state); err != nil {
		return err
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

	// Note that this method writes [batch] to the database.
	if err := a.ctx.SharedMemory.Apply(parentState.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	if onAcceptFunc := parentState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}

	a.ctx.Log.Trace(
		"accepted block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", parentID),
		zap.Stringer("checksum", a.state.Checksum()),
	)

	return nil
}

func (a *acceptor) proposalBlock(b block.Block, blockType string) {
	// Note that:
	//
	// * We don't free the proposal block in this method.
	//   It is freed when its child is accepted.
	//   We need to keep this block's state in memory for its child to use.
	//
	// * We only update the metrics to reflect this block's
	//   acceptance when its child is accepted.
	//
	// * We don't write this block to state here.
	//   That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	//   We do this so that in the event that the node shuts down, the proposal block
	//   is not written to disk unless its child is.
	//   (The VM's Shutdown method commits the database.)
	//   The snowman.Engine requires that the last committed block is a decision block

	blkID := b.ID()
	a.backend.lastAccepted = blkID

	a.ctx.Log.Trace(
		"accepted block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
		zap.Stringer("checksum", a.state.Checksum()),
	)
}

func (a *acceptor) standardBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	defer a.free(blkID)

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
	}

	if err := a.commonAccept(blkState); err != nil {
		return err
	}

	// Update the state to reflect the changes made in [onAcceptState].
	if err := blkState.onAcceptState.Apply(a.state); err != nil {
		return err
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

	// Note that this method writes [batch] to the database.
	if err := a.ctx.SharedMemory.Apply(blkState.atomicRequests, batch); err != nil {
		return fmt.Errorf("failed to apply vm's state to shared memory: %w", err)
	}

	if onAcceptFunc := blkState.onAcceptFunc; onAcceptFunc != nil {
		onAcceptFunc()
	}

	a.ctx.Log.Trace(
		"accepted block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
		zap.Stringer("checksum", a.state.Checksum()),
	)

	return nil
}

func (a *acceptor) commonAccept(b *blockState) error {
	blk := b.statelessBlock
	blkID := blk.ID()

	if err := a.metrics.MarkAccepted(b.metrics); err != nil {
		return fmt.Errorf("failed to accept block %s: %w", blkID, err)
	}

	a.backend.lastAccepted = blkID
	a.state.SetLastAccepted(blkID)
	a.state.SetHeight(blk.Height())
	a.state.AddStatelessBlock(blk)
	a.validators.OnAcceptedBlockID(blkID)
	return nil
}
