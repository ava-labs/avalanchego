// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"slices"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
)

// numShards is the number of mutex shards used to reduce lock contention for
// concurrent Put operations. Using 256 shards provides a good balance between
// memory usage (~2KB) and concurrency.
const numShards = 256

var _ database.HeightIndex = (*cacheDB)(nil)

type cacheDB struct {
	db     *Database
	cache  *lru.Cache[BlockHeight, BlockData]
	shards [numShards]sync.Mutex
}

func newCacheDB(db *Database, size uint16) *cacheDB {
	return &cacheDB{
		db:    db,
		cache: lru.NewCache[BlockHeight, BlockData](int(size)),
	}
}

func (c *cacheDB) Get(height BlockHeight) (BlockData, error) {
	c.db.closeMu.RLock()
	defer c.db.closeMu.RUnlock()

	if c.db.closed {
		c.db.log.Error("Failed Get: database closed", zap.Uint64("height", height))
		return nil, database.ErrClosed
	}

	if cached, ok := c.cache.Get(height); ok {
		return slices.Clone(cached), nil
	}
	data, err := c.db.getWithoutLock(height)
	if err != nil {
		return nil, err
	}
	c.cache.Put(height, slices.Clone(data))
	return data, nil
}

// Put writes block data at the specified height to both the underlying database
// and the cache.
//
// Concurrent calls to Put with the same height are serialized using sharded
// locking to ensure cache consistency with the underlying database.
// This allows concurrent writes to different heights while preventing race
// conditions for writes to the same height.
func (c *cacheDB) Put(height BlockHeight, data BlockData) error {
	shard := &c.shards[height%numShards]
	shard.Lock()
	defer shard.Unlock()

	if err := c.db.Put(height, data); err != nil {
		return err
	}

	c.cache.Put(height, slices.Clone(data))
	return nil
}

func (c *cacheDB) Has(height BlockHeight) (bool, error) {
	c.db.closeMu.RLock()
	defer c.db.closeMu.RUnlock()

	if c.db.closed {
		c.db.log.Error("Failed Has: database closed", zap.Uint64("height", height))
		return false, database.ErrClosed
	}

	if _, ok := c.cache.Get(height); ok {
		return true, nil
	}
	return c.db.hasWithoutLock(height)
}

func (c *cacheDB) Close() error {
	if err := c.db.Close(); err != nil {
		return err
	}
	c.cache.Flush()
	return nil
}
