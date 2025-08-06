// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func cachedBlockSize(_ ids.ID, bw *BlockWrapper) int {
	return ids.IDLen + len(bw.Bytes()) + 2*constants.PointerOverhead
}

func cachedBlockBytesSize(blockBytes string, _ ids.ID) int {
	return len(blockBytes) + ids.IDLen
}

// State implements an efficient caching layer used to wrap a VM
// implementation.
type State struct {
	// getBlock retrieves a block from the VM's storage. If getBlock returns
	// a nil error, then the returned block must not have the status Unknown
	getBlock func(context.Context, ids.ID) (snowman.Block, error)
	// unmarshals [b] into a block
	unmarshalBlock        func(context.Context, []byte) (snowman.Block, error)
	batchedUnmarshalBlock func(context.Context, [][]byte) ([]snowman.Block, error)
	// buildBlock attempts to build a block on top of the currently preferred block
	// buildBlock should always return a block with status Processing since it should never
	// create an unknown block, and building on top of the preferred block should never yield
	// a block that has already been decided.
	buildBlock func(context.Context) (snowman.Block, error)

	// If nil, [BuildBlockWithContext] returns [BuildBlock].
	buildBlockWithContext func(context.Context, *block.Context) (snowman.Block, error)

	// verifiedBlocks is a map of blocks that have been verified and are
	// therefore currently in consensus.
	verifiedBlocks map[ids.ID]*BlockWrapper
	// decidedBlocks is an LRU cache of decided blocks.
	decidedBlocks cache.Cacher[ids.ID, *BlockWrapper]
	// unverifiedBlocks is an LRU cache of blocks with status processing
	// that have not yet passed verification.
	unverifiedBlocks cache.Cacher[ids.ID, *BlockWrapper]
	// missingBlocks is an LRU cache of missing blocks
	missingBlocks cache.Cacher[ids.ID, struct{}]
	// string([byte repr. of block]) --> the block's ID
	bytesToIDCache    cache.Cacher[string, ids.ID]
	lastAcceptedBlock *BlockWrapper
}

// Config defines all of the parameters necessary to initialize State
type Config struct {
	// Cache configuration:
	DecidedCacheSize, MissingCacheSize, UnverifiedCacheSize, BytesToIDCacheSize int

	LastAcceptedBlock     snowman.Block
	GetBlock              func(context.Context, ids.ID) (snowman.Block, error)
	UnmarshalBlock        func(context.Context, []byte) (snowman.Block, error)
	BatchedUnmarshalBlock func(context.Context, [][]byte) ([]snowman.Block, error)
	BuildBlock            func(context.Context) (snowman.Block, error)
	BuildBlockWithContext func(context.Context, *block.Context) (snowman.Block, error)
}

func (s *State) initialize(config *Config) {
	s.verifiedBlocks = make(map[ids.ID]*BlockWrapper)
	s.getBlock = config.GetBlock
	s.buildBlock = config.BuildBlock
	s.buildBlockWithContext = config.BuildBlockWithContext
	s.unmarshalBlock = config.UnmarshalBlock
	s.batchedUnmarshalBlock = config.BatchedUnmarshalBlock
	s.lastAcceptedBlock = &BlockWrapper{
		Block: config.LastAcceptedBlock,
		state: s,
	}
	s.decidedBlocks.Put(config.LastAcceptedBlock.ID(), s.lastAcceptedBlock)
}

func NewState(config *Config) *State {
	c := &State{
		verifiedBlocks:   make(map[ids.ID]*BlockWrapper),
		decidedBlocks:    lru.NewSizedCache(config.DecidedCacheSize, cachedBlockSize),
		missingBlocks:    lru.NewCache[ids.ID, struct{}](config.MissingCacheSize),
		unverifiedBlocks: lru.NewSizedCache(config.UnverifiedCacheSize, cachedBlockSize),
		bytesToIDCache:   lru.NewSizedCache(config.BytesToIDCacheSize, cachedBlockBytesSize),
	}
	c.initialize(config)
	return c
}

func NewMeteredState(
	registerer prometheus.Registerer,
	config *Config,
) (*State, error) {
	decidedCache, err := metercacher.New[ids.ID, *BlockWrapper](
		"decided_cache",
		registerer,
		lru.NewSizedCache(config.DecidedCacheSize, cachedBlockSize),
	)
	if err != nil {
		return nil, err
	}
	missingCache, err := metercacher.New[ids.ID, struct{}](
		"missing_cache",
		registerer,
		lru.NewCache[ids.ID, struct{}](config.MissingCacheSize),
	)
	if err != nil {
		return nil, err
	}
	unverifiedCache, err := metercacher.New[ids.ID, *BlockWrapper](
		"unverified_cache",
		registerer,
		lru.NewSizedCache(config.UnverifiedCacheSize, cachedBlockSize),
	)
	if err != nil {
		return nil, err
	}
	bytesToIDCache, err := metercacher.New[string, ids.ID](
		"bytes_to_id_cache",
		registerer,
		lru.NewSizedCache(config.BytesToIDCacheSize, cachedBlockBytesSize),
	)
	if err != nil {
		return nil, err
	}
	c := &State{
		verifiedBlocks:   make(map[ids.ID]*BlockWrapper),
		decidedBlocks:    decidedCache,
		missingBlocks:    missingCache,
		unverifiedBlocks: unverifiedCache,
		bytesToIDCache:   bytesToIDCache,
	}
	c.initialize(config)
	return c, nil
}

var errSetAcceptedWithProcessing = errors.New("cannot set last accepted block with blocks processing")

// SetLastAcceptedBlock sets the last accepted block to [lastAcceptedBlock].
// This should be called with an internal block - not a wrapped block returned
// from state.
//
// This also flushes [lastAcceptedBlock] from missingBlocks and unverifiedBlocks
// to ensure that their contents stay valid.
func (s *State) SetLastAcceptedBlock(lastAcceptedBlock snowman.Block) error {
	if len(s.verifiedBlocks) != 0 {
		return fmt.Errorf("%w: %d", errSetAcceptedWithProcessing, len(s.verifiedBlocks))
	}

	// [lastAcceptedBlock] is no longer missing or unverified, so we evict it from the corresponding
	// caches.
	//
	// Note: there's no need to evict from the decided blocks cache or bytesToIDCache since their
	// contents will still be valid.
	lastAcceptedBlockID := lastAcceptedBlock.ID()
	s.missingBlocks.Evict(lastAcceptedBlockID)
	s.unverifiedBlocks.Evict(lastAcceptedBlockID)
	s.lastAcceptedBlock = &BlockWrapper{
		Block: lastAcceptedBlock,
		state: s,
	}
	s.decidedBlocks.Put(lastAcceptedBlockID, s.lastAcceptedBlock)

	return nil
}

// Flush each block cache
func (s *State) Flush() {
	s.decidedBlocks.Flush()
	s.missingBlocks.Flush()
	s.unverifiedBlocks.Flush()
	s.bytesToIDCache.Flush()
}

// GetBlock returns the BlockWrapper as snowman.Block corresponding to [blkID]
func (s *State) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	if blk, ok := s.getCachedBlock(blkID); ok {
		return blk, nil
	}

	if _, ok := s.missingBlocks.Get(blkID); ok {
		return nil, database.ErrNotFound
	}

	blk, err := s.getBlock(ctx, blkID)
	// If getBlock returns [database.ErrNotFound], State considers
	// this a cacheable miss.
	if err == database.ErrNotFound {
		s.missingBlocks.Put(blkID, struct{}{})
		return nil, err
	} else if err != nil {
		return nil, err
	}

	// Since this block is not in consensus, addBlockOutsideConsensus
	// is called to add [blk] to the correct cache.
	return s.addBlockOutsideConsensus(blk), nil
}

// getCachedBlock checks the caches for [blkID] by priority. Returning
// true if [blkID] is found in one of the caches.
func (s *State) getCachedBlock(blkID ids.ID) (snowman.Block, bool) {
	if blk, ok := s.verifiedBlocks[blkID]; ok {
		return blk, true
	}

	if blk, ok := s.decidedBlocks.Get(blkID); ok {
		return blk, true
	}

	if blk, ok := s.unverifiedBlocks.Get(blkID); ok {
		return blk, true
	}

	return nil, false
}

// GetBlockInternal returns the internal representation of [blkID]
func (s *State) GetBlockInternal(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	wrappedBlk, err := s.GetBlock(ctx, blkID)
	if err != nil {
		return nil, err
	}

	return wrappedBlk.(*BlockWrapper).Block, nil
}

// ParseBlock attempts to parse [b] into an internal Block and adds it to the
// appropriate caching layer if successful.
func (s *State) ParseBlock(ctx context.Context, b []byte) (snowman.Block, error) {
	// See if we've cached this block's ID by its byte repr.
	cachedBlkID, blkIDCached := s.bytesToIDCache.Get(string(b))
	if blkIDCached {
		// See if we have this block cached
		if cachedBlk, ok := s.getCachedBlock(cachedBlkID); ok {
			return cachedBlk, nil
		}
	}

	// We don't have this block cached by its byte repr.
	// Parse the block from bytes
	blk, err := s.unmarshalBlock(ctx, b)
	if err != nil {
		return nil, err
	}
	blkID := blk.ID()
	s.bytesToIDCache.Put(string(b), blkID)

	// Only check the caches if we didn't do so above
	if !blkIDCached {
		// Check for an existing block, so we can return a unique block
		// if processing or simply allow this block to be immediately
		// garbage collected if it is already cached.
		if cachedBlk, ok := s.getCachedBlock(blkID); ok {
			return cachedBlk, nil
		}
	}

	s.missingBlocks.Evict(blkID)

	// Since this block is not in consensus, addBlockOutsideConsensus
	// is called to add [blk] to the correct cache.
	return s.addBlockOutsideConsensus(blk), nil
}

// BatchedParseBlock implements part of the block.BatchedChainVM interface. In
// addition to performing all the caching as the ParseBlock function, it
// performs at most one call to the underlying VM if [batchedUnmarshalBlock] was
// provided.
func (s *State) BatchedParseBlock(ctx context.Context, blksBytes [][]byte) ([]snowman.Block, error) {
	blks := make([]snowman.Block, len(blksBytes))
	idWasCached := make([]bool, len(blksBytes))
	unparsedBlksBytes := make([][]byte, 0, len(blksBytes))
	for i, blkBytes := range blksBytes {
		// See if we've cached this block's ID by its byte repr.
		blkID, blkIDCached := s.bytesToIDCache.Get(string(blkBytes))
		idWasCached[i] = blkIDCached
		if !blkIDCached {
			unparsedBlksBytes = append(unparsedBlksBytes, blkBytes)
			continue
		}

		// See if we have this block cached
		if cachedBlk, ok := s.getCachedBlock(blkID); ok {
			blks[i] = cachedBlk
		} else {
			unparsedBlksBytes = append(unparsedBlksBytes, blkBytes)
		}
	}

	if len(unparsedBlksBytes) == 0 {
		return blks, nil
	}

	var (
		parsedBlks []snowman.Block
		err        error
	)
	if s.batchedUnmarshalBlock != nil {
		parsedBlks, err = s.batchedUnmarshalBlock(ctx, unparsedBlksBytes)
		if err != nil {
			return nil, err
		}
	} else {
		parsedBlks = make([]snowman.Block, len(unparsedBlksBytes))
		for i, blkBytes := range unparsedBlksBytes {
			parsedBlks[i], err = s.unmarshalBlock(ctx, blkBytes)
			if err != nil {
				return nil, err
			}
		}
	}

	i := 0
	for _, blk := range parsedBlks {
		for ; ; i++ {
			if blks[i] == nil {
				break
			}
		}

		blkID := blk.ID()
		if !idWasCached[i] {
			blkBytes := blk.Bytes()
			blkBytesStr := string(blkBytes)
			s.bytesToIDCache.Put(blkBytesStr, blkID)

			// Check for an existing block, so we can return a unique block
			// if processing or simply allow this block to be immediately
			// garbage collected if it is already cached.
			if cachedBlk, ok := s.getCachedBlock(blkID); ok {
				blks[i] = cachedBlk
				continue
			}
		}

		s.missingBlocks.Evict(blkID)
		blks[i] = s.addBlockOutsideConsensus(blk)
	}
	return blks, nil
}

// BuildBlockWithContext attempts to build a new internal Block, wraps it, and
// adds it to the appropriate caching layer if successful.
// If [s.buildBlockWithContext] is nil, returns [BuildBlock].
func (s *State) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	if s.buildBlockWithContext == nil {
		return s.BuildBlock(ctx)
	}

	blk, err := s.buildBlockWithContext(ctx, blockCtx)
	if err != nil {
		return nil, err
	}

	return s.deduplicate(blk), nil
}

// BuildBlock attempts to build a new internal Block, wraps it, and adds it
// to the appropriate caching layer if successful.
func (s *State) BuildBlock(ctx context.Context) (snowman.Block, error) {
	blk, err := s.buildBlock(ctx)
	if err != nil {
		return nil, err
	}

	return s.deduplicate(blk), nil
}

func (s *State) deduplicate(blk snowman.Block) snowman.Block {
	blkID := blk.ID()
	// Defensive: buildBlock should not return a block that has already been verified.
	// If it does, make sure to return the existing reference to the block.
	if existingBlk, ok := s.getCachedBlock(blkID); ok {
		return existingBlk
	}
	// Evict the produced block from missing blocks in case it was previously
	// marked as missing.
	s.missingBlocks.Evict(blkID)

	// wrap the returned block and add it to the correct cache
	return s.addBlockOutsideConsensus(blk)
}

// addBlockOutsideConsensus adds [blk] to the correct cache and returns
// a wrapped version of [blk]
// assumes [blk] is a known, non-wrapped block that is not currently
// in consensus. [blk] could be either decided or a block that has not yet
// been verified and added to consensus.
func (s *State) addBlockOutsideConsensus(blk snowman.Block) snowman.Block {
	wrappedBlk := &BlockWrapper{
		Block: blk,
		state: s,
	}

	blkID := blk.ID()
	if blk.Height() <= s.lastAcceptedBlock.Height() {
		s.decidedBlocks.Put(blkID, wrappedBlk)
	} else {
		s.unverifiedBlocks.Put(blkID, wrappedBlk)
	}

	return wrappedBlk
}

func (s *State) LastAccepted(context.Context) (ids.ID, error) {
	return s.lastAcceptedBlock.ID(), nil
}

// LastAcceptedBlock returns the last accepted wrapped block
func (s *State) LastAcceptedBlock() *BlockWrapper {
	return s.lastAcceptedBlock
}

// LastAcceptedBlockInternal returns the internal snowman.Block that was last accepted
func (s *State) LastAcceptedBlockInternal() snowman.Block {
	return s.LastAcceptedBlock().Block
}

// IsProcessing returns whether [blkID] is processing in consensus
func (s *State) IsProcessing(blkID ids.ID) bool {
	_, ok := s.verifiedBlocks[blkID]
	return ok
}
