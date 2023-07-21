// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
)

var _ Cacher[struct{}, any] = (*sizedLRU[struct{}, any])(nil)

// sizedLRU is a key value store with bounded size. If the size is attempted to
// be exceeded, then elements are removed from the cache until the bound is
// honored, based on evicting the least recently used value.
type sizedLRU[K comparable, V any] struct {
	lock        sync.Mutex
	elements    linkedhashmap.LinkedHashmap[K, V]
	maxSize     int
	currentSize int
	size        func(V) int
}

func NewSizedLRU[K comparable, V any](maxSize int, size func(V) int) Cacher[K, V] {
	return &sizedLRU[K, V]{
		elements: linkedhashmap.New[K, V](),
		maxSize:  maxSize,
		size:     size,
	}
}

func (c *sizedLRU[K, V]) Put(key K, value V) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

func (c *sizedLRU[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

func (c *sizedLRU[K, V]) Evict(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

func (c *sizedLRU[K, V]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *sizedLRU[_, _]) PortionFilled() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.portionFilled()
}

func (c *sizedLRU[K, V]) put(key K, value V) {
	valueSize := c.size(value)
	if valueSize > c.maxSize {
		c.flush()
		return
	}

	if oldValue, ok := c.elements.Get(key); ok {
		c.currentSize -= c.size(oldValue)
	}

	// Remove elements until the size of elements in the cache <= [c.maxSize].
	for c.currentSize > c.maxSize-valueSize {
		oldestKey, value, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
		c.currentSize -= c.size(value)
	}

	c.elements.Put(key, value)
	c.currentSize += valueSize
}

func (c *sizedLRU[K, V]) get(key K) (V, bool) {
	value, ok := c.elements.Get(key)
	if !ok {
		return utils.Zero[V](), false
	}

	c.elements.Put(key, value) // Mark [k] as MRU.
	return value, true
}

func (c *sizedLRU[K, _]) evict(key K) {
	if value, ok := c.elements.Get(key); ok {
		c.elements.Delete(key)
		c.currentSize -= c.size(value)
	}
}

func (c *sizedLRU[K, V]) flush() {
	c.elements = linkedhashmap.New[K, V]()
	c.currentSize = 0
}

func (c *sizedLRU[_, _]) portionFilled() float64 {
	return float64(c.currentSize) / float64(c.maxSize)
}
