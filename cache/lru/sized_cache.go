// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linked"
)

var _ cache.Cacher[struct{}, any] = (*sizedCache[struct{}, any])(nil)

// sizedCache is a key value store with bounded size. If the size is attempted
// to be exceeded, then elements are removed from the cache until the bound is
// honored, based on evicting the least recently used value.
type sizedCache[K comparable, V any] struct {
	lock        sync.Mutex
	elements    *linked.Hashmap[K, V]
	maxSize     int
	currentSize int
	size        func(K, V) int
}

func NewSizedCache[K comparable, V any](maxSize int, size func(K, V) int) cache.Cacher[K, V] {
	return &sizedCache[K, V]{
		elements: linked.NewHashmap[K, V](),
		maxSize:  maxSize,
		size:     size,
	}
}

func (c *sizedCache[K, V]) Put(key K, value V) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

func (c *sizedCache[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

func (c *sizedCache[K, V]) Evict(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

func (c *sizedCache[K, V]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *sizedCache[_, _]) Len() int {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.len()
}

func (c *sizedCache[_, _]) PortionFilled() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.portionFilled()
}

func (c *sizedCache[K, V]) put(key K, value V) {
	newEntrySize := c.size(key, value)
	if newEntrySize > c.maxSize {
		c.flush()
		return
	}

	if oldValue, ok := c.elements.Get(key); ok {
		c.currentSize -= c.size(key, oldValue)
	}

	// Remove elements until the size of elements in the cache <= [c.maxSize].
	for c.currentSize > c.maxSize-newEntrySize {
		oldestKey, oldestValue, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
		c.currentSize -= c.size(oldestKey, oldestValue)
	}

	c.elements.Put(key, value)
	c.currentSize += newEntrySize
}

func (c *sizedCache[K, V]) get(key K) (V, bool) {
	value, ok := c.elements.Get(key)
	if !ok {
		return utils.Zero[V](), false
	}

	c.elements.Put(key, value) // Mark [k] as MRU.
	return value, true
}

func (c *sizedCache[K, _]) evict(key K) {
	if value, ok := c.elements.Get(key); ok {
		c.elements.Delete(key)
		c.currentSize -= c.size(key, value)
	}
}

func (c *sizedCache[K, V]) flush() {
	c.elements.Clear()
	c.currentSize = 0
}

func (c *sizedCache[_, _]) len() int {
	return c.elements.Len()
}

func (c *sizedCache[_, _]) portionFilled() float64 {
	return float64(c.currentSize) / float64(c.maxSize)
}
