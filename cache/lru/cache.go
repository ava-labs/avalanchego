// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linked"
)

var _ cache.Cacher[struct{}, struct{}] = (*Cache[struct{}, struct{}])(nil)

// Cache is a key value store with bounded size. If the size is attempted to be
// exceeded, then an element is removed from the cache before the insertion is
// done, based on evicting the least recently used value.
type Cache[K comparable, V any] struct {
	lock     sync.Mutex
	elements *linked.Hashmap[K, V]
	size     int

	// onEvict is called with the key and value of an entry before eviction, if set.
	onEvict func(K, V)
}

// NewCache creates a new LRU cache with the given size.
func NewCache[K comparable, V any](size int) *Cache[K, V] {
	return &Cache[K, V]{
		elements: linked.NewHashmap[K, V](),
		size:     max(size, 1),
		onEvict:  nil,
	}
}

// NewCacheWithCallback creates a new LRU cache with the given size and eviction callback.
// The onEvict callback is called with the key and value of an entry before eviction.
// The onEvict callback is called while holding the cache lock.
// Do not call any cache methods (Get, Put, Evict, Flush) from within the callback
// as this will cause a deadlock. The callback should only be used for cleanup
// operations like closing files or releasing resources.
func NewCacheWithCallback[K comparable, V any](size int, onEvict func(K, V)) *Cache[K, V] {
	return &Cache[K, V]{
		elements: linked.NewHashmap[K, V](),
		size:     max(size, 1),
		onEvict:  onEvict,
	}
}

func (c *Cache[K, V]) Put(key K, value V) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.elements.Len() == c.size {
		oldestKey, oldestValue, found := c.elements.Oldest()
		if c.onEvict != nil && found {
			c.onEvict(oldestKey, oldestValue)
		}
		c.elements.Delete(oldestKey)
	}
	c.elements.Put(key, value)
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	val, ok := c.elements.Get(key)
	if !ok {
		return utils.Zero[V](), false
	}
	c.elements.Put(key, val) // Mark [k] as MRU.
	return val, true
}

func (c *Cache[K, _]) Evict(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.onEvict != nil {
		if value, found := c.elements.Get(key); found {
			c.onEvict(key, value)
		}
	}
	c.elements.Delete(key)
}

func (c *Cache[_, _]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	// Call onEvict for each element before clearing
	if c.onEvict != nil {
		iter := c.elements.NewIterator()
		for iter.Next() {
			c.onEvict(iter.Key(), iter.Value())
		}
	}

	c.elements.Clear()
}

func (c *Cache[_, _]) Len() int {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.elements.Len()
}

func (c *Cache[_, _]) PortionFilled() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	return float64(c.elements.Len()) / float64(c.size)
}
