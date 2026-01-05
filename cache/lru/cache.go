// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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

	// onEvict is called with the key and value of an entry before eviction.
	onEvict func(K, V)
}

// NewCache creates a new LRU cache with the given size.
func NewCache[K comparable, V any](size int) *Cache[K, V] {
	return NewCacheWithOnEvict(size, func(K, V) {})
}

// NewCacheWithOnEvict creates a new LRU cache with the given size and eviction callback.
func NewCacheWithOnEvict[K comparable, V any](size int, onEvict func(K, V)) *Cache[K, V] {
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
		if oldestKey, oldestValue, ok := c.elements.Oldest(); ok {
			c.evict(oldestKey, oldestValue)
		}
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

	value, ok := c.elements.Get(key)
	if !ok {
		return
	}

	c.evict(key, value)
}

func (c *Cache[K, V]) evict(key K, value V) {
	c.onEvict(key, value)
	c.elements.Delete(key)
}

func (c *Cache[_, _]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	for iter := c.elements.NewIterator(); iter.Next(); {
		c.evict(iter.Key(), iter.Value())
	}
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
