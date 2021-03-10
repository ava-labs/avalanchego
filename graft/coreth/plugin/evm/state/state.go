// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	errUnknownBlock = errors.New("unknown block")
)

// UnmarshalType ...
type UnmarshalType func([]byte) (Block, error)

// BuildBlockType ...
type BuildBlockType func() (Block, error)

// GetBlockType ...
type GetBlockType func(ids.ID) (Block, error)

// GetBlockIDAtHeightType ...
type GetBlockIDAtHeightType func(uint64) (ids.ID, error)

// ChainState defines the canonical state of the chain
// it tracks the accepted blocks and wraps a VM's implementation
// of snowman.Block in order to take care of writing blocks to
// the database and adding a caching layer for both the blocks
// and their statuses.
type ChainState struct {
	// baseDB is the base level database used to make
	// atomic commits on block acceptance.
	baseDB *versiondb.Database

	// ChainState keeps these function types to request operations
	// from the VM implementation.
	getBlockIDAtHeight GetBlockIDAtHeightType
	getBlock           GetBlockType
	unmarshalBlock     UnmarshalType
	buildBlock         BuildBlockType
	genesisBlock       snowman.Block

	// processingBlocks are the verified blocks that have entered
	// consensus
	processingBlocks map[ids.ID]*BlockWrapper
	// decidedBlocks is an LRU cache of decided blocks.
	decidedBlocks *cache.LRU
	// unverifiedBlocks is an LRU cache of blocks with status processing
	// that have not yet been verified.
	unverifiedBlocks *cache.LRU
	// missingBlocks is an LRU cache of missing blocks
	missingBlocks     *cache.LRU
	lastAcceptedBlock *BlockWrapper
}

// NewChainState ...
func NewChainState(db database.Database, cacheSize int) *ChainState {
	baseDB := versiondb.New(db)

	state := &ChainState{
		baseDB:           baseDB,
		processingBlocks: make(map[ids.ID]*BlockWrapper, cacheSize),
		decidedBlocks:    &cache.LRU{Size: cacheSize},
		missingBlocks:    &cache.LRU{Size: cacheSize},
		unverifiedBlocks: &cache.LRU{Size: cacheSize},
	}
	return state
}

// Initialize sets the genesis block, last accepted block, and the functions for retrieving blocks from the VM layer.
func (c *ChainState) Initialize(lastAcceptedBlock Block, getBlockIDAtHeight GetBlockIDAtHeightType, getBlock GetBlockType, unmarshalBlock UnmarshalType, buildBlock BuildBlockType) {
	// Set the functions for retrieving blocks from the VM
	c.getBlockIDAtHeight = getBlockIDAtHeight
	c.getBlock = getBlock
	c.unmarshalBlock = unmarshalBlock
	c.buildBlock = buildBlock

	lastAcceptedBlock.SetStatus(choices.Accepted)
	c.lastAcceptedBlock = &BlockWrapper{
		Block: lastAcceptedBlock,
		state: c,
	}
	c.lastAcceptedBlock.SetStatus(choices.Accepted)
	c.decidedBlocks.Put(lastAcceptedBlock.ID(), c.lastAcceptedBlock)
}

// FlushCaches flushes each block cache completely.
func (c *ChainState) FlushCaches() {
	c.decidedBlocks.Flush()
	c.missingBlocks.Flush()
	c.unverifiedBlocks.Flush()
}

// ExternalDB returns a database to be used external to ChainState
// Any operations that occur on the returned database during Accept/Reject
// of blocks will be automatically batched by ChainState.
func (c *ChainState) ExternalDB() database.Database { return c.baseDB }

// GetBlock returns the BlockWrapper as snowman.Block corresponding to [blkID]
func (c *ChainState) GetBlock(blkID ids.ID) (snowman.Block, error) {
	if blk, ok := c.getCachedBlock(blkID); ok {
		return blk, nil
	}

	if _, ok := c.missingBlocks.Get(blkID); ok {
		return nil, errUnknownBlock
	}

	blk, err := c.getBlock(blkID)
	if err != nil {
		c.missingBlocks.Put(blkID, struct{}{})
		return nil, err
	}

	// Since this block is not in processing, we can add it as
	// a non-processing block to the correct cache.
	return c.addNonProcessingBlock(blk)
}

// getCachedBlock checks the caches for [blkID] by priority. Returning
// true if [blkID] is found in one of the caches.
func (c *ChainState) getCachedBlock(blkID ids.ID) (snowman.Block, bool) {
	if blk, ok := c.processingBlocks[blkID]; ok {
		return blk, true
	}

	if blk, ok := c.decidedBlocks.Get(blkID); ok {
		return blk.(snowman.Block), true
	}

	if blk, ok := c.unverifiedBlocks.Get(blkID); ok {
		return blk.(snowman.Block), true
	}

	return nil, false
}

// GetBlockInternal returns the internal representation of [blkID]
func (c *ChainState) GetBlockInternal(blkID ids.ID) (Block, error) {
	wrappedBlk, err := c.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	return wrappedBlk.(*BlockWrapper).Block, nil
}

// ParseBlock attempts to parse [b] into an internal Block and adds it to the appropriate
// caching layer if successful.
func (c *ChainState) ParseBlock(b []byte) (snowman.Block, error) {
	blk, err := c.unmarshalBlock(b)
	if err != nil {
		return nil, err
	}

	blkID := blk.ID()
	// Check for an existing block, so we can return a unique block
	// if processing or simply allow this block to be immediately
	// garbage collected if it is already cached.
	if cachedBlk, ok := c.getCachedBlock(blkID); ok {
		return cachedBlk, nil
	}

	c.missingBlocks.Evict(blkID)

	// Since we've checked above if it's in processing we can add [blk]
	// as a non-processing block here.
	return c.addNonProcessingBlock(blk)
}

// BuildBlock attempts to build a new internal Block, wraps it, and adds it
// to the appropriate caching layer if successful.
func (c *ChainState) BuildBlock() (snowman.Block, error) {
	blk, err := c.buildBlock()
	if err != nil {
		return nil, err
	}

	// Evict the produced block from missing blocks.
	c.missingBlocks.Evict(blk.ID())

	// Blocks built by BuildBlock are built on top of the
	// preferred block, so it is guaranteed to be in Processing.
	blk.SetStatus(choices.Processing)
	wrappedBlk := &BlockWrapper{
		Block: blk,
		state: c,
	}
	// Since the consensus engine has not called Verify on this
	// block yet, we can add it directly as a non-processing block.
	c.unverifiedBlocks.Put(blk.ID(), wrappedBlk)
	return wrappedBlk, nil
}

// addNonProcessingBlock adds [blk] to the correct cache and returns
// a wrapped version of [blk]
// assumes [blk] is a known, non-wrapped block that is not currently
// being processed in consensus.
// assumes lock is held.
func (c *ChainState) addNonProcessingBlock(blk Block) (snowman.Block, error) {
	wrappedBlk := &BlockWrapper{
		Block: blk,
		state: c,
	}

	status, err := c.getStatus(blk)
	if err != nil {
		return nil, err
	}

	blk.SetStatus(status)
	blkID := blk.ID()
	switch status {
	case choices.Accepted, choices.Rejected:
		c.decidedBlocks.Put(blkID, wrappedBlk)
	case choices.Processing:
		c.unverifiedBlocks.Put(blk.ID(), wrappedBlk)
	default:
		return nil, fmt.Errorf("found unexpected status for blk %s: %s", blkID, status)
	}

	return wrappedBlk, nil
}

// LastAccepted ...
func (c *ChainState) LastAccepted() (ids.ID, error) {
	return c.lastAcceptedBlock.ID(), nil
}

// LastAcceptedBlock returns the last accepted wrapped block
func (c *ChainState) LastAcceptedBlock() *BlockWrapper {
	return c.lastAcceptedBlock
}

// LastAcceptedBlockInternal returns the internal snowman.Block that was last last accepted
func (c *ChainState) LastAcceptedBlockInternal() snowman.Block {
	return c.LastAcceptedBlock().Block
}

// getStatus returns the status of [blk]. Assumes that [blk] is a known block.
// assumes lock is held.
func (c *ChainState) getStatus(blk snowman.Block) (choices.Status, error) {
	blkHeight := blk.Height()
	lastAcceptedHeight := c.lastAcceptedBlock.Height()
	if blkHeight > lastAcceptedHeight {
		return choices.Processing, nil
	}

	// Get the blockID at [blkHeight] so it can be compared to [blk]
	acceptedBlkID, err := c.getBlockIDAtHeight(blk.Height())
	if err != nil {
		return choices.Unknown, fmt.Errorf("failed to get acceptedID at height %d, below last accepted height: %w", blk.Height(), err)
	}

	if acceptedBlkID == blk.ID() {
		return choices.Accepted, nil
	}

	return choices.Rejected, nil
}
