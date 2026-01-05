// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var errEmptyCacheTooLarge = errors.New("cache is empty yet still too large")

// A cache that calls [onEviction] on the evicted element.
type onEvictCache[K comparable, V any] struct {
	lock        sync.RWMutex
	maxSize     int
	currentSize int
	fifo        *linked.Hashmap[K, V]
	size        func(K, V) int
	// Must not call any method that grabs [c.lock]
	// because this would cause a deadlock.
	onEviction func(K, V) error
}

// [size] must always return a positive number.
func newOnEvictCache[K comparable, V any](
	maxSize int,
	size func(K, V) int,
	onEviction func(K, V) error,
) onEvictCache[K, V] {
	return onEvictCache[K, V]{
		maxSize:    maxSize,
		fifo:       linked.NewHashmap[K, V](),
		size:       size,
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
// and returns the error from [c.onEviction]. Otherwise, returns nil.
func (c *onEvictCache[K, V]) Put(key K, value V) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if oldValue, replaced := c.fifo.Get(key); replaced {
		c.currentSize -= c.size(key, oldValue)
	}

	c.currentSize += c.size(key, value)
	c.fifo.Put(key, value) // Mark as MRU

	return c.resize(c.maxSize)
}

// Flush removes all elements from the cache.
//
// Returns the first non-nil error returned by [c.onEviction], if any.
//
// If [c.onEviction] errors, it will still be called for any subsequent elements
// and the cache will still be emptied.
func (c *onEvictCache[K, V]) Flush() error {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.resize(0)
}

// removeOldest returns and removes the oldest element from this cache.
//
// Assumes [c.lock] is held.
func (c *onEvictCache[K, V]) removeOldest() (K, V, bool) {
	k, v, exists := c.fifo.Oldest()
	if exists {
		c.currentSize -= c.size(k, v)
		c.fifo.Delete(k)
	}
	return k, v, exists
}

// resize removes the oldest elements from the cache until the cache is not
// larger than the provided target.
//
// Assumes [c.lock] is held.
func (c *onEvictCache[K, V]) resize(target int) error {
	// Note that we can't use [c.fifo]'s iterator because [c.onEviction]
	// modifies [c.fifo], which violates the iterator's invariant.
	var errs wrappers.Errs
	for c.currentSize > target {
		k, v, exists := c.removeOldest()
		if !exists {
			// This should really never happen unless the size of an entry
			// changed or the target size is negative.
			return errEmptyCacheTooLarge
		}
		errs.Add(c.onEviction(k, v))
	}
	return errs.Err
}
