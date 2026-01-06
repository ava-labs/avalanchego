// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block           = (*BlockWrapper)(nil)
	_ block.WithVerifyContext = (*BlockWrapper)(nil)

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

// BlockWrapper wraps a snowman Block while adding a smart caching layer to improve
// VM performance.
type BlockWrapper struct {
	snowman.Block

	state *State
}

// Verify verifies the underlying block, evicts from the unverified block cache
// and if the block passes verification, adds it to [cache.verifiedBlocks].
// Note: it is guaranteed that if a block passes verification it will be added to
// consensus and eventually be decided ie. either Accept/Reject will be called
// on [bw] removing it from [verifiedBlocks].
func (bw *BlockWrapper) Verify(ctx context.Context) error {
	if err := bw.Block.Verify(ctx); err != nil {
		// Note: we cannot cache blocks failing verification in case
		// the error is temporary and the block could become valid in
		// the future.
		return err
	}

	blkID := bw.ID()
	bw.state.unverifiedBlocks.Evict(blkID)
	bw.state.verifiedBlocks[blkID] = bw
	return nil
}

// ShouldVerifyWithContext checks if the underlying block should be verified
// with a block context. If the underlying block does not implement the
// block.WithVerifyContext interface, returns false without an error. Does not
// touch any block cache.
func (bw *BlockWrapper) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := bw.Block.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}
	return blkWithCtx.ShouldVerifyWithContext(ctx)
}

// VerifyWithContext verifies the underlying block with the given block context,
// evicts from the unverified block cache and if the block passes verification,
// adds it to [cache.verifiedBlocks].
// Note: it is guaranteed that if a block passes verification it will be added
// to consensus and eventually be decided ie. either Accept/Reject will be
// called on [bw] removing it from [verifiedBlocks].
//
// Note: If the underlying block does not implement the block.WithVerifyContext
// interface, an error is always returned because ShouldVerifyWithContext will
// always return false in this case and VerifyWithContext should never be
// called.
func (bw *BlockWrapper) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	blkWithCtx, ok := bw.Block.(block.WithVerifyContext)
	if !ok {
		return fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, bw.Block)
	}

	if err := blkWithCtx.VerifyWithContext(ctx, blockCtx); err != nil {
		// Note: we cannot cache blocks failing verification in case
		// the error is temporary and the block could become valid in
		// the future.
		return err
	}

	blkID := bw.ID()
	bw.state.unverifiedBlocks.Evict(blkID)
	bw.state.verifiedBlocks[blkID] = bw
	return nil
}

// Accept accepts the underlying block, removes it from verifiedBlocks, caches it as a decided
// block, and updates the last accepted block.
func (bw *BlockWrapper) Accept(ctx context.Context) error {
	blkID := bw.ID()
	delete(bw.state.verifiedBlocks, blkID)
	bw.state.decidedBlocks.Put(blkID, bw)
	bw.state.lastAcceptedBlock = bw

	return bw.Block.Accept(ctx)
}

// Reject rejects the underlying block, removes it from processing blocks, and caches it as a
// decided block.
func (bw *BlockWrapper) Reject(ctx context.Context) error {
	blkID := bw.ID()
	delete(bw.state.verifiedBlocks, blkID)
	bw.state.decidedBlocks.Put(blkID, bw)
	return bw.Block.Reject(ctx)
}
