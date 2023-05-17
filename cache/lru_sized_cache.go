// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
)

type SizedElement interface {
	Size() int
}

// var _ Cacher[struct{}, struct{}] = (*SizedLRU[struct{}, struct{}])(nil)

// LRU is a key value store with bounded size. If the size is attempted to be
// exceeded, then an element is removed from the cache before the insertion is
// done, based on evicting the least recently used value.
type SizedLRU[K comparable, V SizedElement] struct {
	lock     sync.Mutex
	elements linkedhashmap.LinkedHashmap[K, V]
	// If set to < 0, will be set internally to 1.
	MaxSize     int
	currentSize int
}

func (c *SizedLRU[K, V]) Put(key K, value V) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

func (c *SizedLRU[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

func (c *SizedLRU[K, V]) Evict(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

func (c *SizedLRU[K, V]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *SizedLRU[K, V]) put(key K, value V) {
	c.resize()

	for c.currentSize > c.MaxSize {
		oldestKey, value, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
		c.currentSize -= value.Size()
	}
	c.elements.Put(key, value)
}

func (c *SizedLRU[K, V]) get(key K) (V, bool) {
	c.resize()

	val, ok := c.elements.Get(key)
	if !ok {
		return utils.Zero[V](), false
	}
	c.elements.Put(key, val) // Mark [k] as MRU.
	return val, true
}

func (c *SizedLRU[K, _]) evict(key K) {
	c.resize()

	c.elements.Delete(key)
}

func (c *SizedLRU[K, V]) flush() {
	c.elements = linkedhashmap.New[K, V]()
}

// Initializes [c.elements] if it's nil.
// Sets [c.size] to 1 if it's <= 0.
// Removes oldest elements to make number of elements
// in the cache == [c.size] if necessary.
func (c *SizedLRU[K, V]) resize() {
	if c.elements == nil {
		c.elements = linkedhashmap.New[K, V]()
	}
	if c.MaxSize <= 0 {
		c.MaxSize = 1
	}
	for c.currentSize > c.MaxSize {
		oldestKey, value, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
		c.currentSize -= value.Size()
	}
}
