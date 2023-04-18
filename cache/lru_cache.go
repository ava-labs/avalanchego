// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
)

var _ Cacher[struct{}, struct{}] = (*LRU[struct{}, struct{}])(nil)

// LRU is a key value store with bounded size. If the size is attempted to be
// exceeded, then an element is removed from the cache before the insertion is
// done, based on evicting the least recently used value.
type LRU[K comparable, V any] struct {
	lock     sync.Mutex
	elements linkedhashmap.LinkedHashmap[K, V]
	// If set to < 0, will be set internally to 1.
	Size int
}

func (c *LRU[K, V]) Put(key K, value V) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

func (c *LRU[K, _]) Evict(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

func (c *LRU[_, _]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *LRU[K, V]) put(key K, value V) {
	c.resize()

	if c.elements.Len() == c.Size {
		oldestKey, _, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
	}
	c.elements.Put(key, value)
}

func (c *LRU[K, V]) get(key K) (V, bool) {
	c.resize()

	val, ok := c.elements.Get(key)
	if !ok {
		return utils.Zero[V](), false
	}
	c.elements.Put(key, val) // Mark [k] as MRU.
	return val, true
}

func (c *LRU[K, _]) evict(key K) {
	c.resize()

	c.elements.Delete(key)
}

func (c *LRU[K, V]) flush() {
	c.elements = linkedhashmap.New[K, V]()
}

// Initializes [c.elements] if it's nil.
// Sets [c.size] to 1 if it's <= 0.
// Removes oldest elements to make number of elements
// in the cache == [c.size] if necessary.
func (c *LRU[K, V]) resize() {
	if c.elements == nil {
		c.elements = linkedhashmap.New[K, V]()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
	for c.elements.Len() > c.Size {
		oldestKey, _, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
	}
}
