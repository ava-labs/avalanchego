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
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

var (
	_ snowman.Block       = (*Block)(nil)
	_ snowman.OracleBlock = (*Block)(nil)
)

// Exported for testing in platformvm package.
type Block struct {
	block.Block
	manager                  *manager
	rejected                 bool
	regossipRejectedBlockTxs bool
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
	blkID := b.ID()
	defer b.manager.free(blkID)

	b.manager.ctx.Log.Verbo(
		"rejecting block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.Height()),
		zap.Stringer("parentID", b.Parent()),
	)

	if !b.regossipRejectedBlockTxs {
		return nil
	}

	for _, tx := range b.Txs() {
		if err := b.manager.Mempool.Add(tx); err != nil {
			b.manager.ctx.Log.Debug(
				"failed to reissue tx",
				zap.Stringer("txID", tx.ID()),
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
		}
	}

	b.rejected = true
	return nil
}

func (b *Block) Status() choices.Status {
	// If this block's reference was rejected, we should report it as rejected.
	//
	// We don't persist the rejection, but that's fine. The consensus engine
	// will hold the same reference to the block until it no longer needs it.
	// After the consensus engine has released the reference to the block that
	// was verified, it may get a new reference that isn't marked as rejected.
	// The consensus engine may then try to issue the block, but will discover
	// that it was rejected due to a conflicting block having been accepted.
	if b.rejected {
		return choices.Rejected
	}

	blkID := b.ID()
	// If this block is the last accepted block, we don't need to go to disk to
	// check the status.
	if b.manager.lastAccepted == blkID {
		return choices.Accepted
	}
	// Check if the block is in memory. If so, it's processing.
	if _, ok := b.manager.blkIDToState[blkID]; ok {
		return choices.Processing
	}
	// Block isn't in memory. Check in the database.
	_, err := b.manager.state.GetStatelessBlock(blkID)
	switch err {
	case nil:
		return choices.Accepted

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
