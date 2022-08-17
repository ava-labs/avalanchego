// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

var (
	_ snowman.Block       = &Block{}
	_ snowman.OracleBlock = &Block{}
)

// Exported for testing in platformvm package.
type Block struct {
	blocks.Block
	manager *manager
}

func (b *Block) Verify() error {
	return b.Visit(b.manager.verifier)
}

func (b *Block) Accept() error {
	return b.Visit(b.manager.acceptor)
}

func (b *Block) Reject() error {
	return b.Visit(b.manager.rejector)
}

func (b *Block) Status() choices.Status {
	blkID := b.ID()
	// If this block is an accepted Proposal block with no accepted children, it
	// will be in [blkIDToState], but we should return accepted, not processing,
	// so we do this check.
	if b.manager.backend.lastAccepted == blkID {
		return choices.Accepted
	}
	// Check if the block is in memory. If so, it's processing.
	if _, ok := b.manager.backend.blkIDToState[blkID]; ok {
		return choices.Processing
	}
	// Block isn't in memory. Check in the database.
	if _, status, err := b.manager.state.GetStatelessBlock(blkID); err == nil {
		return status
	}
	// choices.Unknown means we don't have the bytes of the block.
	// In this case, we do, so we return choices.Processing.
	return choices.Processing
}

func (b *Block) Timestamp() time.Time {
	return b.manager.getTimestamp(b.Block)
}

func (b *Block) Options() ([2]snowman.Block, error) {
	var (
		statelessCommitBlk blocks.Block
		statelessAbortBlk  blocks.Block
		err                error

		blkID      = b.ID()
		nextHeight = b.Height() + 1
	)

	switch blk := b.Block.(type) {
	case *blocks.ApricotProposalBlock:
		statelessCommitBlk, err = blocks.NewApricotCommitBlock(blkID, nextHeight)
		if err != nil {
			return [2]snowman.Block{}, fmt.Errorf(
				"failed to create commit block: %w",
				err,
			)
		}

		statelessAbortBlk, err = blocks.NewApricotAbortBlock(blkID, nextHeight)
		if err != nil {
			return [2]snowman.Block{}, fmt.Errorf(
				"failed to create abort block: %w",
				err,
			)
		}

	case *blocks.BlueberryProposalBlock:
		timestamp := blk.Timestamp()
		statelessCommitBlk, err = blocks.NewBlueberryCommitBlock(timestamp, blkID, nextHeight)
		if err != nil {
			return [2]snowman.Block{}, fmt.Errorf(
				"failed to create commit block: %w",
				err,
			)
		}

		statelessAbortBlk, err = blocks.NewBlueberryAbortBlock(timestamp, blkID, nextHeight)
		if err != nil {
			return [2]snowman.Block{}, fmt.Errorf(
				"failed to create abort block: %w",
				err,
			)
		}

	default:
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	commitBlock := b.manager.NewBlock(statelessCommitBlk)
	abortBlock := b.manager.NewBlock(statelessAbortBlk)

	blkState, ok := b.manager.backend.blkIDToState[blkID]
	if !ok {
		return [2]snowman.Block{}, fmt.Errorf("block %s state not found", blkID)
	}

	if blkState.initiallyPreferCommit {
		return [2]snowman.Block{commitBlock, abortBlock}, nil
	}
	return [2]snowman.Block{abortBlock, commitBlock}, nil
}
