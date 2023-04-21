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
	lock       sync.RWMutex
	maxSize    int
	fifo       linkedhashmap.LinkedHashmap[K, V]
	onEviction func(V) error
}

func newOnEvictCache[K comparable, V any](maxSize int, onEviction func(V) error) onEvictCache[K, V] {
	return onEvictCache[K, V]{
		maxSize:    maxSize,
		fifo:       linkedhashmap.New[K, V](),
		onEviction: onEviction,
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
		oldestKey, oldsetVal, _ := c.fifo.Oldest()
		c.fifo.Delete(oldestKey)
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
		c.fifo = linkedhashmap.New[K, V]()
		c.lock.Unlock()
	}()

	var errs wrappers.Errs
	iter := c.fifo.NewIterator()
	for iter.Next() {
		val := iter.Value()
		errs.Add(c.onEviction(val))
	}
	return errs.Err
}
