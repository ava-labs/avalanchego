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
	lock    sync.Mutex
	maxSize int
	// LRU --> MRU from left to right.
	lru        linkedhashmap.LinkedHashmap[K, V]
	onEviction func(V) error
}

func newOnEvictCache[K comparable, V any](maxSize int, onEviction func(V) error) onEvictCache[K, V] {
	return onEvictCache[K, V]{
		maxSize:    maxSize,
		lru:        linkedhashmap.New[K, V](),
		onEviction: onEviction,
	}
}

// Get an element from this cache.
func (c *onEvictCache[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	val, ok := c.lru.Get(key)
	if ok {
		// This key was touched; move it to the MRU position.
		c.lru.Put(key, val)
	}
	return val, ok
}

// Put an element into this cache. If this causes an element
// to be evicted, calls [c.onEviction] on the evicted element
// and returns the error from [c.onEviction]. Otherwise returns nil.
func (c *onEvictCache[K, V]) Put(key K, value V) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.lru.Put(key, value) // Mark as MRU

	if c.lru.Len() > c.maxSize {
		// Note that [c.cache] has already evicted the oldest
		// element because its max size is [c.maxSize].
		oldestKey, oldsetVal, _ := c.lru.Oldest()
		c.lru.Delete(oldestKey)
		return c.onEviction(oldsetVal)
	}
	return nil
}

// Removes all elements from the cache.
// Returns the last non-nil error during [c.onEviction], if any.
// If [c.onEviction] errors, it will still be called for any
// subsequent elements and the cache will still be emptied.
func (c *onEvictCache[K, V]) Flush() error {
	c.lock.Lock()
	defer func() {
		c.lru = linkedhashmap.New[K, V]()
		c.lock.Unlock()
	}()

	var errs wrappers.Errs
	iter := c.lru.NewIterator()
	for iter.Next() {
		val := iter.Value()
		errs.Add(c.onEviction(val))
	}
	return errs.Err
}
