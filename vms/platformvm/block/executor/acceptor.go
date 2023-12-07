// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
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
	metrics      metrics.Metrics
	validators   validators.Manager
	bootstrapped *utils.Atomic[bool]
}

func (a *acceptor) BanffAbortBlock(b *block.BanffAbortBlock) error {
	return a.abortBlock(b, "banff abort")
}

func (a *acceptor) BanffCommitBlock(b *block.BanffCommitBlock) error {
	return a.commitBlock(b, "apricot commit")
}

func (a *acceptor) BanffProposalBlock(b *block.BanffProposalBlock) error {
	return a.proposalBlock(b, "banff proposal")
}

func (a *acceptor) BanffStandardBlock(b *block.BanffStandardBlock) error {
	return a.standardBlock(b, "banff standard")
}

func (a *acceptor) ApricotAbortBlock(b *block.ApricotAbortBlock) error {
	return a.abortBlock(b, "apricot abort")
}

func (a *acceptor) ApricotCommitBlock(b *block.ApricotCommitBlock) error {
	return a.commitBlock(b, "apricot commit")
}

func (a *acceptor) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	return a.proposalBlock(b, "apricot proposal")
}

func (a *acceptor) ApricotStandardBlock(b *block.ApricotStandardBlock) error {
	return a.standardBlock(b, "apricot standard")
}

func (a *acceptor) ApricotAtomicBlock(b *block.ApricotAtomicBlock) error {
	blkID := b.ID()
	defer a.free(blkID)

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
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
		zap.Stringer("utxoChecksum", a.state.Checksum()),
	)

	return nil
}

func (a *acceptor) abortBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
	}

	if a.bootstrapped.Get() {
		if blkState.initiallyPreferCommit {
			a.metrics.MarkOptionVoteLost()
		} else {
			a.metrics.MarkOptionVoteWon()
		}
	}

	return a.optionBlock(b, blockType)
}

func (a *acceptor) commitBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
	}

	if a.bootstrapped.Get() {
		if blkState.initiallyPreferCommit {
			a.metrics.MarkOptionVoteWon()
		} else {
			a.metrics.MarkOptionVoteLost()
		}
	}

	return a.optionBlock(b, blockType)
}

func (a *acceptor) optionBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	parentID := b.Parent()

	defer func() {
		a.free(parentID)
		a.free(blkID)
	}()

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
	}

	if err := blkState.onAcceptState.Apply(a.state); err != nil {
		return err
	}

	// Here we commit both option block changes and its proposal block
	// parent ones.
	if err := a.state.Commit(); err != nil {
		return err
	}

	a.ctx.Log.Trace(
		"accepted block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", parentID),
		zap.Stringer("utxoChecksum", a.state.Checksum()),
	)

	return nil
}

func (a *acceptor) proposalBlock(b block.Block, blockType string) error {
	blkID := b.ID()

	// Note: we free proposalBlock blkState entry once we accept an options
	// TODO: consider handling cases when this can be done earlier
	// (e.g. both options verified before accepting one of them)

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
	}
	if err := blkState.onAcceptState.Apply(a.state); err != nil {
		return err
	}

	// Note: We don't write this block to state here.
	//       That is done when this block's child (a CommitBlock or AbortBlock) is accepted.
	//       We do this so that in the event that the node shuts down, the proposal block
	//       is not written to disk unless its child is.
	//       (The VM's Shutdown method commits the database.)
	//       The snowman.Engine requires that the last committed block is a decision block

	a.backend.lastAccepted = blkID

	a.ctx.Log.Trace(
		"accepted block",
		zap.String("blockType", blockType),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
		zap.Stringer("utxoChecksum", a.state.Checksum()),
	)
	return nil
}

func (a *acceptor) standardBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	defer a.free(blkID)

	if err := a.commonAccept(b); err != nil {
		return err
	}

	blkState, ok := a.blkIDToState[blkID]
	if !ok {
		return fmt.Errorf("%w %s", errMissingBlockState, blkID)
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
		zap.Stringer("utxoChecksum", a.state.Checksum()),
	)

	return nil
}

func (a *acceptor) commonAccept(b block.Block) error {
	blkID := b.ID()

	if err := a.metrics.MarkAccepted(b); err != nil {
		return fmt.Errorf("failed to accept block %s: %w", blkID, err)
	}

	a.backend.lastAccepted = blkID
	a.state.SetLastAccepted(blkID)
	a.state.SetHeight(b.Height())
	a.state.AddStatelessBlock(b)
	a.validators.OnAcceptedBlockID(blkID)
	return nil
}
