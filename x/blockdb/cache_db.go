// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
)

var _ database.HeightIndex = (*cacheDB)(nil)

// cacheDB caches data from the underlying [Database].
//
// Operations (Get, Has, Put) are not atomic with the underlying database.
// Concurrent writes to the same height can result in cache inconsistencies where
// the cache and database contain different values. This limitation is acceptable
// because concurrent writes to the same height are not an intended use case.
type cacheDB struct {
	db     *Database
	cache  *lru.Cache[BlockHeight, BlockData]
	closed atomic.Bool
}

func newCacheDB(db *Database, size uint16) *cacheDB {
	return &cacheDB{
		db:    db,
		cache: lru.NewCache[BlockHeight, BlockData](int(size)),
	}
}

func (c *cacheDB) Get(height BlockHeight) (BlockData, error) {
	if c.closed.Load() {
		c.db.log.Error("Failed Get: database closed", zap.Uint64("height", height))
		return nil, database.ErrClosed
	}

	if cached, ok := c.cache.Get(height); ok {
		return cached, nil
	}
	data, err := c.db.Get(height)
	if err != nil {
		return nil, err
	}
	c.cache.Put(height, data)
	return data, nil
}

func (c *cacheDB) Put(height BlockHeight, data BlockData) error {
	if c.closed.Load() {
		c.db.log.Error("Failed Put: database closed", zap.Uint64("height", height))
		return database.ErrClosed
	}

	if err := c.db.Put(height, data); err != nil {
		return err
	}

	c.cache.Put(height, data)
	return nil
}

func (c *cacheDB) Has(height BlockHeight) (bool, error) {
	if c.closed.Load() {
		c.db.log.Error("Failed Has: database closed", zap.Uint64("height", height))
		return false, database.ErrClosed
	}

	if _, ok := c.cache.Get(height); ok {
		return true, nil
	}
	return c.db.Has(height)
}

func (c *cacheDB) Sync(start, end uint64) error {
	if c.closed.Load() {
		return database.ErrClosed
	}
	return c.db.Sync(start, end)
}

func (c *cacheDB) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return database.ErrClosed
	}
	c.cache.Flush()
	return c.db.Close()
}
