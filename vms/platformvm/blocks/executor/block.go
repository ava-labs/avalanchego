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
	if b.manager.lastAccepted == blkID {
		return choices.Accepted
	}
	// Check if the block is in memory. If so, it's processing.
	if _, ok := b.manager.blkIDToState[blkID]; ok {
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
	// If this is the last accepted block and the block was loaded from disk
	// since it was accepted, then the timestamp wouldn't be set correctly. So,
	// we explicitly return the chain time.
	// Check if the block is processing.
	if blkState, ok := b.manager.blkIDToState[b.ID()]; ok {
		return blkState.timestamp
	}
	// The block isn't processing.
	// According to the snowman.Block interface, the last accepted
	// block is the only accepted block that must return a correct timestamp,
	// so we just return the chain time.
	return b.manager.state.GetTimestamp()
}

func (b *Block) Options() ([2]snowman.Block, error) {
	if _, ok := b.Block.(*blocks.ApricotProposalBlock); !ok {
		return [2]snowman.Block{}, snowman.ErrNotOracle
	}

	blkID := b.ID()
	nextHeight := b.Height() + 1

	statelessCommitBlk, err := blocks.NewApricotCommitBlock(
		blkID,
		nextHeight,
	)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}
	commitBlock := b.manager.NewBlock(statelessCommitBlk)

	statelessAbortBlk, err := blocks.NewApricotAbortBlock(
		blkID,
		nextHeight,
	)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}
	abortBlock := b.manager.NewBlock(statelessAbortBlk)

	blkState, ok := b.manager.blkIDToState[blkID]
	if !ok {
		return [2]snowman.Block{}, fmt.Errorf("block %s state not found", blkID)
	}

	if blkState.initiallyPreferCommit {
		return [2]snowman.Block{commitBlock, abortBlock}, nil
	}
	return [2]snowman.Block{abortBlock, commitBlock}, nil
}
