// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

var (
	_ snowman.Block       = (*Block)(nil)
	_ snowman.OracleBlock = (*Block)(nil)
)

// Exported for testing in platformvm package.
type Block struct {
	blocks.Block
	manager *manager
}

func (b *Block) Verify(context.Context) error {
	blkID := b.ID()
	if _, ok := b.manager.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	return b.Visit(b.manager.verifier)
}

func (b *Block) Accept(context.Context) error {
	return b.Visit(b.manager.acceptor)
}

func (b *Block) Reject(context.Context) error {
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
	_, status, err := b.manager.state.GetStatelessBlock(blkID)
	switch err {
	case nil:
		return status

	case database.ErrNotFound:
		// choices.Unknown means we don't have the bytes of the block.
		// In this case, we do, so we return choices.Processing.
		return choices.Processing

	default:
		// TODO: correctly report this error to the consensus engine.
		b.manager.ctx.Log.Error(
			"dropping unhandled database error",
			zap.Error(err),
		)
		return choices.Processing
	}
}

func (b *Block) Timestamp() time.Time {
	return b.manager.getTimestamp(b.ID())
}

func (b *Block) Options(context.Context) ([2]snowman.Block, error) {
	options := options{}
	if err := b.Block.Visit(&options); err != nil {
		return [2]snowman.Block{}, err
	}

	commitBlock := b.manager.NewBlock(options.commitBlock)
	abortBlock := b.manager.NewBlock(options.abortBlock)

	blkID := b.ID()
	blkState, ok := b.manager.blkIDToState[blkID]
	if !ok {
		return [2]snowman.Block{}, fmt.Errorf("block %s state not found", blkID)
	}

	if blkState.initiallyPreferCommit {
		return [2]snowman.Block{commitBlock, abortBlock}, nil
	}
	return [2]snowman.Block{abortBlock, commitBlock}, nil
}
