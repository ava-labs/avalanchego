// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
)

// BlockWrapper wraps a snowman Block while adding a smart caching layer to improve
// VM performance.
type BlockWrapper struct {
	snowman.Block

	cache *Cache
}

// Verify verifies the underlying block, evicts from the unverified block cache
// and if the block passes verification, adds it to [cache.verifiedBlocks].
// Note: it is guaranteed that if a block passes verification it will be added to
// consensus and eventually be decided ie. either Accept/Reject will be called
// on [bw] removing it from [verifiedBlocks].
func (bw *BlockWrapper) Verify() error {
	blkID := bw.ID()
	bw.cache.unverifiedBlocks.Evict(blkID)

	err := bw.Block.Verify()
	if err != nil {
		// Note: we cannot cache blocks failing verification in case
		// the error is temporary and the block could become valid in
		// the future.
		return err
	}
	bw.cache.verifiedBlocks[blkID] = bw
	return nil
}

// Accept accepts the underlying block, removes it from verifiedBlocks, caches it as a decided
// block, and updates the last acceptd block.
func (bw *BlockWrapper) Accept() error {
	blkID := bw.ID()
	delete(bw.cache.verifiedBlocks, blkID)
	bw.cache.decidedBlocks.Put(blkID, bw)
	bw.cache.lastAcceptedBlock = bw

	return bw.Block.Accept()
}

// Reject rejects the underlying block, removes it from processing blocks, and caches it as a
// decided block.
func (bw *BlockWrapper) Reject() error {
	blkID := bw.ID()
	delete(bw.cache.verifiedBlocks, blkID)
	bw.cache.decidedBlocks.Put(blkID, bw)
	return bw.Block.Reject()
}

// Parent returns the parent of [bw]
// Ensures that a BlockWrapper is returned instead of the internal
// block type by using cache to retrieve the parent block by ID.
func (bw *BlockWrapper) Parent() snowman.Block {
	parentID := bw.Block.Parent().ID()
	blk, err := bw.cache.GetBlock(parentID)
	if err == nil {
		return blk
	}
	return &missing.Block{BlkID: parentID}
}
