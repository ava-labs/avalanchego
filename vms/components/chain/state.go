// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/prometheus/client_golang/prometheus"
)

// Add GetBlockIDAtHeight to support
type VM interface {
	block.ChainVM

	// Add GetBlockIDAtHeight to support ChainState block status lookups.
	GetBlockIDAtHeight(height uint64) (ids.ID, error)
}

var (
	// ErrBlockNotFound indicates that the VM was not able to retrieve a block. If this error is returned
	// from getBlock then the miss will be considered cacheable. Any other error will not be considered a
	// cacheable miss.
	ErrBlockNotFound = errors.New("block not found")
)

// State defines the canonical state of the chain
// it tracks the accepted blocks and wraps a VM's implementation
// of snowman.Block in order to take care of writing blocks to
// the database and adding a caching layer for both the blocks
// and their statuses.
type State struct {
	// baseDB is the base level database used to make
	// atomic commits on block acceptance.
	baseDB *versiondb.Database

	// State keeps these function types to request operations
	// from the VM implementation.
	getBlockIDAtHeight func(uint64) (ids.ID, error)
	// getBlock retrieves a block from the VM's storage. If getBlock returns
	// a nil error, then the returned block must not have the status Unknown
	getBlock       func(ids.ID) (Block, error)
	unmarshalBlock func([]byte) (Block, error)
	buildBlock     func() (Block, error)

	// verifiedBlocks are the verified blocks that have entered
	// consensus
	verifiedBlocks map[ids.ID]*BlockWrapper
	// decidedBlocks is an LRU cache of decided blocks.
	decidedBlocks cache.Cacher
	// unverifiedBlocks is an LRU cache of blocks with status processing
	// that have not yet been verified.
	unverifiedBlocks cache.Cacher
	// missingBlocks is an LRU cache of missing blocks
	missingBlocks     cache.Cacher
	lastAcceptedBlock *BlockWrapper
}

// Config defines all of the parameters necessary to initialize State
type Config struct {
	LastAcceptedBlock  Block
	GetBlockIDAtHeight func(uint64) (ids.ID, error)
	GetBlock           func(ids.ID) (Block, error)
	UnmarshalBlock     func([]byte) (Block, error)
	BuildBlock         func() (Block, error)
}

// NewState returns a new uninitialized State
func NewState(db database.Database, decidedCacheSize, missingCacheSize, unverifiedCacheSize int) *State {
	return &State{
		baseDB:           versiondb.New(db),
		verifiedBlocks:   make(map[ids.ID]*BlockWrapper),
		decidedBlocks:    &cache.LRU{Size: decidedCacheSize},
		missingBlocks:    &cache.LRU{Size: missingCacheSize},
		unverifiedBlocks: &cache.LRU{Size: unverifiedCacheSize},
	}
}

func NewMeteredState(
	db database.Database,
	registerer prometheus.Registerer,
	namespace string,
	decidedCacheSize,
	missingCacheSize,
	unverifiedCacheSize int,
) (*State, error) {
	decidedCache, err := metercacher.New(
		fmt.Sprintf("%s_decided_cache", namespace),
		registerer,
		&cache.LRU{Size: decidedCacheSize},
	)
	if err != nil {
		return nil, err
	}
	missingCache, err := metercacher.New(
		fmt.Sprintf("%s_missing_cache", namespace),
		registerer,
		&cache.LRU{Size: missingCacheSize},
	)
	if err != nil {
		return nil, err
	}
	unverifiedCache, err := metercacher.New(
		fmt.Sprintf("%s_unverified_cache", namespace),
		registerer,
		&cache.LRU{Size: unverifiedCacheSize},
	)
	if err != nil {
		return nil, err
	}

	return &State{
		baseDB:           versiondb.New(db),
		verifiedBlocks:   make(map[ids.ID]*BlockWrapper),
		decidedBlocks:    decidedCache,
		missingBlocks:    missingCache,
		unverifiedBlocks: unverifiedCache,
	}, nil
}

// Initialize sets the last accepted block, and the internal functions for retrieving/parsing/building
// blocks from the VM layer.
func (c *State) Initialize(config *Config) {
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
func (c *State) FlushCaches() {
	c.decidedBlocks.Flush()
	c.missingBlocks.Flush()
	c.unverifiedBlocks.Flush()
}

// ExternalDB returns a database to be used external to State
// Any operations that occur on the returned database during Accept/Reject
// of blocks will be automatically batched by State.
func (c *State) ExternalDB() database.Database { return c.baseDB }

// GetBlock returns the BlockWrapper as snowman.Block corresponding to [blkID]
func (c *State) GetBlock(blkID ids.ID) (snowman.Block, error) {
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

	// Since this block is not in consensus, addBlockOutsideConsensus
	// is called to add [blk] to the correct cache.
	return c.addBlockOutsideConsensus(blk)
}

// getCachedBlock checks the caches for [blkID] by priority. Returning
// true if [blkID] is found in one of the caches.
func (c *State) getCachedBlock(blkID ids.ID) (snowman.Block, bool) {
	if blk, ok := c.verifiedBlocks[blkID]; ok {
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
func (c *State) GetBlockInternal(blkID ids.ID) (Block, error) {
	wrappedBlk, err := c.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	return wrappedBlk.(*BlockWrapper).Block, nil
}

// ParseBlock attempts to parse [b] into an internal Block and adds it to the appropriate
// caching layer if successful.
func (c *State) ParseBlock(b []byte) (snowman.Block, error) {
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

	// Since this block is not in consensus, addBlockOutsideConsensus
	// is called to add [blk] to the correct cache.
	return c.addBlockOutsideConsensus(blk)
}

// BuildBlock attempts to build a new internal Block, wraps it, and adds it
// to the appropriate caching layer if successful.
func (c *State) BuildBlock() (snowman.Block, error) {
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

// addBlockOutsideConsensus adds [blk] to the correct cache and returns
// a wrapped version of [blk]
// assumes [blk] is a known, non-wrapped block that is not currently
// in consensus. [blk] could be either decided or a block that has not yet
// been verified and added to consensus.
func (c *State) addBlockOutsideConsensus(blk Block) (snowman.Block, error) {
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
func (c *State) LastAccepted() (ids.ID, error) {
	return c.lastAcceptedBlock.ID(), nil
}

// LastAcceptedBlock returns the last accepted wrapped block
func (c *State) LastAcceptedBlock() *BlockWrapper {
	return c.lastAcceptedBlock
}

// LastAcceptedBlockInternal returns the internal snowman.Block that was last last accepted
func (c *State) LastAcceptedBlockInternal() snowman.Block {
	return c.LastAcceptedBlock().Block
}

// GetBlockIDAtHeight returns the blockID at the given height by passing through to the internal
// function.
func (c *State) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	return c.getBlockIDAtHeight(height)
}

// getStatus returns the status of [blk]. Assumes that [blk] is a known block.
func (c *State) getStatus(blk snowman.Block) (choices.Status, error) {
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
