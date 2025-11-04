// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"slices"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
)

var _ database.HeightIndex = (*cacheDB)(nil)

// cacheDB caches data from the underlying database.
//
// Operations (Get, Has, Put) are not atomic with the underlying database.
// Concurrent writes to the same height can result in cache inconsistencies where
// the cache and database contain different values. This limitation is acceptable
// because concurrent writes to the same height are not an intended use case.
type cacheDB struct {
	db    *Database
	cache *lru.Cache[BlockHeight, BlockData]
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

func (c *cacheDB) Put(height BlockHeight, data BlockData) error {
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
