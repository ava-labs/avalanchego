// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// A cache that calls [onEviction] on the evicted element.
type onEvictCache[K comparable, V any] struct {
	lock              sync.RWMutex
	maxSize           int
	evictionBatchSize int
	fifo              linkedhashmap.LinkedHashmap[K, V]
	onEviction        func([]V) error
}

func newOnEvictCache[K comparable, V any](maxSize int, evictionBatchSize int, onEviction func([]V) error) onEvictCache[K, V] {
	return onEvictCache[K, V]{
		maxSize:           maxSize,
		fifo:              linkedhashmap.New[K, V](),
		onEviction:        onEviction,
		evictionBatchSize: evictionBatchSize,
	}
}

// Get an element from this cache.
func (c *onEvictCache[K, V]) Get(key K) (V, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.fifo.Get(key)
}

// Put an element into this cache. If this causes an element
// to be evicted, calls [c.onEviction] on the evicted element
// and returns the error from [c.onEviction]. Otherwise returns nil.
func (c *onEvictCache[K, V]) Put(key K, value V) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.fifo.Put(key, value) // Mark as MRU

	if c.fifo.Len() > c.maxSize {
		evictedValues := make([]V, c.evictionBatchSize)
		for i := 0; i < c.evictionBatchSize; i++ {
			oldestKey, oldestVal, _ := c.fifo.Oldest()
			c.fifo.Delete(oldestKey)
			evictedValues[i] = oldestVal
		}
		return c.onEviction(evictedValues)
	}
	return nil
}

// Flush removes all elements from the cache.
// Returns the last non-nil error during [c.onEviction], if any.
// If [c.onEviction] errors, it will still be called for any
// subsequent elements and the cache will still be emptied.
func (c *onEvictCache[K, V]) Flush() error {
	c.lock.Lock()
	defer func() {
		c.fifo = linkedhashmap.New[K, V]()
		c.lock.Unlock()
	}()

	var errs wrappers.Errs
	iter := c.fifo.NewIterator()
	evictedValues := make([]V, 0, c.evictionBatchSize)
	for iter.Next() {
		val := iter.Value()
		evictedValues = append(evictedValues, val)
		if len(evictedValues) == c.evictionBatchSize {
			errs.Add(c.onEviction(evictedValues))
			evictedValues = make([]V, 0, c.evictionBatchSize)
		}
	}
	errs.Add(c.onEviction(evictedValues))

	return errs.Err
}
