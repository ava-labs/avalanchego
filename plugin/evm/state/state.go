// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"

	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	// ErrBlockNotFound indicates that the VM was not able to retrieve a block. If this error is returned
	// from getBlock then the miss will be considered cacheable. Any other error will not be considered a
	// cacheable miss.
	ErrBlockNotFound = errors.New("block not found")
)

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
	getBlockIDAtHeight func(uint64) (ids.ID, error)
	getBlock           func(ids.ID) (Block, error)
	unmarshalBlock     func([]byte) (Block, error)
	buildBlock         func() (Block, error)

	// processingBlocks are the verified blocks that have entered
	// consensus
	processingBlocks map[ids.ID]*BlockWrapper
	// decidedBlocks is an LRU cache of decided blocks.
	decidedBlocks cache.Cacher
	// unverifiedBlocks is an LRU cache of blocks with status processing
	// that have not yet been verified.
	unverifiedBlocks cache.Cacher
	// missingBlocks is an LRU cache of missing blocks
	missingBlocks     cache.Cacher
	lastAcceptedBlock *BlockWrapper
}

// Config defines all of the parameters necessary to initialize ChainState
type Config struct {
	LastAcceptedBlock  Block
	GetBlockIDAtHeight func(uint64) (ids.ID, error)
	GetBlock           func(ids.ID) (Block, error)
	UnmarshalBlock     func([]byte) (Block, error)
	BuildBlock         func() (Block, error)
}

// NewChainState returns a new uninitialized ChainState
func NewChainState(db database.Database, cacheSize int) *ChainState {
	return &ChainState{
		baseDB:           versiondb.New(db),
		processingBlocks: make(map[ids.ID]*BlockWrapper, cacheSize),
		decidedBlocks:    &cache.LRU{Size: cacheSize},
		missingBlocks:    &cache.LRU{Size: cacheSize},
		unverifiedBlocks: &cache.LRU{Size: cacheSize},
	}
}

// Initialize sets the last accepted block, and the internal functions for retrieving/parsing/building
// blocks from the VM layer.
func (c *ChainState) Initialize(config *Config) {
	// Set the functions for retrieving blocks from the VM
	c.getBlockIDAtHeight = config.GetBlockIDAtHeight
	c.getBlock = config.GetBlock
	c.unmarshalBlock = config.UnmarshalBlock
	c.buildBlock = config.BuildBlock

	config.LastAcceptedBlock.SetStatus(choices.Accepted)
	c.lastAcceptedBlock = &BlockWrapper{
		Block: config.LastAcceptedBlock,
		state: c,
	}
	c.decidedBlocks.Put(config.LastAcceptedBlock.ID(), c.lastAcceptedBlock)
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
		return nil, ErrBlockNotFound
	}

	blk, err := c.getBlock(blkID)
	if err == ErrBlockNotFound {
		c.missingBlocks.Put(blkID, struct{}{})
		return nil, err
	} else if err != nil {
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

	blkID := blk.ID()
	// Evict the produced block from missing blocks.
	c.missingBlocks.Evict(blkID)

	// Blocks built by BuildBlock are built on top of the
	// preferred block, so it is guaranteed to be in Processing.
	blk.SetStatus(choices.Processing)
	wrappedBlk := &BlockWrapper{
		Block: blk,
		state: c,
	}
	// Since the consensus engine has not called Verify on this
	// block yet, we can add it directly as a non-processing block.
	c.unverifiedBlocks.Put(blkID, wrappedBlk)
	return wrappedBlk, nil
}

// addNonProcessingBlock adds [blk] to the correct cache and returns
// a wrapped version of [blk]
// assumes [blk] is a known, non-wrapped block that is not currently
// being processed in consensus.
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
		c.unverifiedBlocks.Put(blkID, wrappedBlk)
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
