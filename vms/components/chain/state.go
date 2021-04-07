// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/prometheus/client_golang/prometheus"
)

// // VM defines an updated VM interface to include GetBlockIDAtHeight. Any VM
// // that implements this interface can be easily wrapped with the caching layer
// // defined by State.
// type VM interface {
// 	block.ChainVM

// 	// Add GetBlockIDAtHeight to support ChainState block status lookups.
// 	GetBlockIDAtHeight(height uint64) (ids.ID, error)
// }

var (
	// ErrBlockNotFound indicates that the VM was not able to retrieve a block. If this error is returned
	// from getBlock then the miss will be considered cacheable. Any other error will not be considered a
	// cacheable miss.
	ErrBlockNotFound = errors.New("block not found")
)

// State implements an efficient caching layer used to wrap a VM
// implementation.
type ChainCache struct {
	// // getBlockIDAtHeight returns the blkID at the given height. If this height is less than or
	// // equal to the last accepted block, then the returned blkID should be guaranteed to be accepted.
	// getBlockIDAtHeight func(uint64) (ids.ID, error)
	// getBlock retrieves a block from the VM's storage. If getBlock returns
	// a nil error, then the returned block must not have the status Unknown
	getBlock func(ids.ID) (snowman.Block, error)
	// unmarshals [b] into a block
	unmarshalBlock func([]byte) (snowman.Block, error)
	// buildBlock attempts to build a block on top of the currently preferred block
	// buildBlock should always return a block with status Processing since it should never
	// create an unknown block, and building on top of the preferred block should never yield
	// a block that has already been decided.
	buildBlock func() (snowman.Block, error)

	// verifiedBlocks is a map of blocks that have been verified and are
	// therefore currently in consensus.
	verifiedBlocks map[ids.ID]*BlockWrapper
	// decidedBlocks is an LRU cache of decided blocks.
	// Every value in [decidedBlocks] is a (*BlockWrapper)
	decidedBlocks cache.Cacher
	// unverifiedBlocks is an LRU cache of blocks with status processing
	// that have not yet been verified.
	// Every value in [unverifiedBlocks] is a (*BlockWrapper)
	unverifiedBlocks cache.Cacher
	// missingBlocks is an LRU cache of missing blocks
	// Every value in [missingBlocks] is an empty struct.
	missingBlocks     cache.Cacher
	lastAcceptedBlock *BlockWrapper
}

// Config defines all of the parameters necessary to initialize State
type Config struct {
	// Cache configuration:
	DecidedCacheSize, MissingCacheSize, UnverifiedCacheSize int

	LastAcceptedBlock snowman.Block
	// GetBlockIDAtHeight func(uint64) (ids.ID, error)
	GetBlock       func(ids.ID) (snowman.Block, error)
	UnmarshalBlock func([]byte) (snowman.Block, error)
	BuildBlock     func() (snowman.Block, error)
}

func NewChainCache(config *Config) *ChainCache {
	c := &ChainCache{
		verifiedBlocks:   make(map[ids.ID]*BlockWrapper),
		decidedBlocks:    &cache.LRU{Size: config.DecidedCacheSize},
		missingBlocks:    &cache.LRU{Size: config.MissingCacheSize},
		unverifiedBlocks: &cache.LRU{Size: config.UnverifiedCacheSize},
		getBlock:         config.GetBlock,
		unmarshalBlock:   config.UnmarshalBlock,
		buildBlock:       config.BuildBlock,
	}
	c.lastAcceptedBlock = &BlockWrapper{
		Block: config.LastAcceptedBlock,
		state: c,
	}
	c.decidedBlocks.Put(config.LastAcceptedBlock.ID(), c.lastAcceptedBlock)
	return c
}

func NewMeteredChainCache(
	registerer prometheus.Registerer,
	namespace string,
	config *Config,
) (*ChainCache, error) {
	decidedCache, err := metercacher.New(
		fmt.Sprintf("%s_decided_cache", namespace),
		registerer,
		&cache.LRU{Size: config.DecidedCacheSize},
	)
	if err != nil {
		return nil, err
	}
	missingCache, err := metercacher.New(
		fmt.Sprintf("%s_missing_cache", namespace),
		registerer,
		&cache.LRU{Size: config.MissingCacheSize},
	)
	if err != nil {
		return nil, err
	}
	unverifiedCache, err := metercacher.New(
		fmt.Sprintf("%s_unverified_cache", namespace),
		registerer,
		&cache.LRU{Size: config.UnverifiedCacheSize},
	)
	if err != nil {
		return nil, err
	}
	c := &ChainCache{
		verifiedBlocks:   make(map[ids.ID]*BlockWrapper),
		decidedBlocks:    decidedCache,
		missingBlocks:    missingCache,
		unverifiedBlocks: unverifiedCache,
		getBlock:         config.GetBlock,
		unmarshalBlock:   config.UnmarshalBlock,
		buildBlock:       config.BuildBlock,
	}
	c.lastAcceptedBlock = &BlockWrapper{
		Block: config.LastAcceptedBlock,
		state: c,
	}
	c.decidedBlocks.Put(config.LastAcceptedBlock.ID(), c.lastAcceptedBlock)
	return c, nil
}

// // Initialize sets the last accepted block, and the internal functions for retrieving/parsing/building
// // blocks from the VM layer.
// func (c *ChainCache) Initialize(config *Config) {
// 	// Set the functions for retrieving blocks from the VM
// 	s.getBlockIDAtHeight = config.GetBlockIDAtHeight
// 	s.getBlock = config.GetBlock
// 	c.unmarshalBlock = config.UnmarshalBlock
// 	s.buildBlock = config.BuildBlock

// 	config.LastAcceptedBlock.SetStatus(choices.Accepted)
// 	c.lastAcceptedBlock = &BlockWrapper{
// 		Block: config.LastAcceptedBlock,
// 		state: s,
// 	}
// 	c.decidedBlocks.Put(config.LastAcceptedBlock.ID(), c.lastAcceptedBlock)
// }

// FlushCaches flushes each block cache completely.
func (c *ChainCache) FlushCaches() {
	c.decidedBlocks.Flush()
	c.missingBlocks.Flush()
	c.unverifiedBlocks.Flush()
}

// GetBlock returns the BlockWrapper as snowman.Block corresponding to [blkID]
func (c *ChainCache) GetBlock(blkID ids.ID) (snowman.Block, error) {
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
func (c *ChainCache) getCachedBlock(blkID ids.ID) (snowman.Block, bool) {
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
func (c *ChainCache) GetBlockInternal(blkID ids.ID) (snowman.Block, error) {
	wrappedBlk, err := c.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	return wrappedBlk.(*BlockWrapper).Block, nil
}

// ParseBlock attempts to parse [b] into an internal Block and adds it to the appropriate
// caching layer if successful.
func (c *ChainCache) ParseBlock(b []byte) (snowman.Block, error) {
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
func (c *ChainCache) BuildBlock() (snowman.Block, error) {
	blk, err := c.buildBlock()
	if err != nil {
		return nil, err
	}

	blkID := blk.ID()
	// Defensive: buildBlock should not return a block that has already been verfied.
	// If it does, make sure to return the existing reference to the block.
	if existingBlk, ok := c.verifiedBlocks[blkID]; ok {
		return existingBlk, nil
	}
	// Evict the produced block from missing blocks in case it was previously
	// marked as missing.
	c.missingBlocks.Evict(blkID)

	// wrap the returned block and add it to the correct cache
	return c.addBlockOutsideConsensus(blk)
}

// addBlockOutsideConsensus adds [blk] to the correct cache and returns
// a wrapped version of [blk]
// assumes [blk] is a known, non-wrapped block that is not currently
// in consensus. [blk] could be either decided or a block that has not yet
// been verified and added to consensus.
func (c *ChainCache) addBlockOutsideConsensus(blk snowman.Block) (snowman.Block, error) {
	wrappedBlk := &BlockWrapper{
		Block: blk,
		state: c,
	}

	blkID := blk.ID()
	status := blk.Status()
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
func (c *ChainCache) LastAccepted() (ids.ID, error) {
	return c.lastAcceptedBlock.ID(), nil
}

// LastAcceptedBlock returns the last accepted wrapped block
func (c *ChainCache) LastAcceptedBlock() *BlockWrapper {
	return c.lastAcceptedBlock
}

// LastAcceptedBlockInternal returns the internal snowman.Block that was last last accepted
func (c *ChainCache) LastAcceptedBlockInternal() snowman.Block {
	return c.LastAcceptedBlock().Block
}

// // GetBlockIDAtHeight returns the blockID at the given height by passing through to the internal
// // function.
// func (c *ChainCache) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
// 	return s.getBlockIDAtHeight(height)
// }

// // getStatus returns the status of [blk]. Assumes that [blk] is a known block.
// func (c *ChainCache) getStatus(blk snowman.Block) (choices.Status, error) {
// 	blkHeight := blk.Height()
// 	lastAcceptedHeight := c.lastAcceptedBlock.Height()
// 	if blkHeight > lastAcceptedHeight {
// 		return choices.Processing, nil
// 	}

// 	// Get the blockID at [blkHeight] so it can be compared to [blk]
// 	acceptedBlkID, err := s.getBlockIDAtHeight(blk.Height())
// 	if err != nil {
// 		return choices.Unknown, fmt.Errorf("failed to get acceptedID at height %d, below last accepted height: %w", blk.Height(), err)
// 	}

// 	if acceptedBlkID == blk.ID() {
// 		return choices.Accepted, nil
// 	}

// 	return choices.Rejected, nil
// }
