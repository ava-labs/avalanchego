// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
)

// Block is the internal representation of a Block to be wrapped by BlockWrapper
type Block interface {
	snowman.Block
	// SetStatus sets the internal status of an existing block. This is used by ChainState
	// to allow internal blocks to keep their status up to date.
	SetStatus(choices.Status)
}

// BlockWrapper wraps a snowman Block and adds a smart caching layer
type BlockWrapper struct {
	Block

	state *ChainState
}

// Verify verifies the underlying block, evicts from the unverified block cache
// and if the block passes verification, adds it to [state] processingBlocks.
// Note: it is guaranteed that if a block passes verification it will be added to
// consensus and have either Accept or Reject called on it.
func (bw *BlockWrapper) Verify() error {
	blkID := bw.ID()
	bw.state.unverifiedBlocks.Evict(blkID)

	err := bw.Block.Verify()
	if err != nil {
		return err
	}
	bw.state.processingBlocks[blkID] = bw
	return nil
}

// Accept accepts the underlying block, removes it from processingBlocks, caches it as a decided
// block, sets the last accepted block, and commits the underlying database atomically
// after block acceptance has been performed.
func (bw *BlockWrapper) Accept() error {
	// TODO set flag on baseDB so that operations taking place within [bw]
	// are placed into the versiondb batch correctly.
	if err := bw.Block.Accept(); err != nil {
		return err
	}

	blkID := bw.ID()
	delete(bw.state.processingBlocks, blkID)
	bw.state.decidedBlocks.Put(blkID, bw)
	bw.state.lastAcceptedBlock = bw

	return bw.state.baseDB.Commit()
}

// Reject rejects the underlying block, removes it from processing blocks, caches it as a decided
// block, sets the last accepted block, and commits the underlying database atomically after the
// block has been rejected.
func (bw *BlockWrapper) Reject() error {
	// TODO set flag on baseDB so that operations taking place within [bw]
	// are placed into the versiondb batch correctly.
	if err := bw.Block.Reject(); err != nil {
		return err
	}

	delete(bw.state.processingBlocks, bw.ID())
	bw.state.decidedBlocks.Put(bw.ID(), bw)
	return bw.state.baseDB.Commit()
}

// Parent returns the parent of [bw]
// Ensures that a BlockWrapper is returned instead of the internal
// block type by using state to retrieve the block.
func (bw *BlockWrapper) Parent() snowman.Block {
	parentID := bw.Block.Parent().ID()
	blk, err := bw.state.GetBlock(parentID)
	if err == nil {
		return blk
	}
	return &missing.Block{BlkID: parentID}
}
