// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linked"
)

var _ Cacher[struct{}, any] = (*sizedLRU[struct{}, any])(nil)

// sizedElement is used to store the element with its size, so we don't
// calculate the size multiple times.
//
// This ensures that any inconsistencies returned by the size function can not
// corrupt the cache.
type sizedElement[V any] struct {
	value V
	size  int
}

// sizedLRU is a key value store with bounded size. If the size is attempted to
// be exceeded, then elements are removed from the cache until the bound is
// honored, based on evicting the least recently used value.
type sizedLRU[K comparable, V any] struct {
	lock        sync.Mutex
	elements    *linked.Hashmap[K, *sizedElement[V]]
	maxSize     int
	currentSize int
	size        func(K, V) int
}

func NewSizedLRU[K comparable, V any](maxSize int, size func(K, V) int) Cacher[K, V] {
	return &sizedLRU[K, V]{
		elements: linked.NewHashmap[K, *sizedElement[V]](),
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

func (c *sizedLRU[_, _]) Len() int {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.len()
}

func (c *sizedLRU[_, _]) PortionFilled() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.portionFilled()
}

func (c *sizedLRU[K, V]) put(key K, value V) {
	newEntrySize := c.size(key, value)
	if newEntrySize > c.maxSize {
		c.flush()
		return
	}

	if oldElement, ok := c.elements.Get(key); ok {
		c.currentSize -= oldElement.size
	}

	// Remove elements until the size of elements in the cache <= [c.maxSize].
	for c.currentSize > c.maxSize-newEntrySize {
		oldestKey, oldestElement, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
		c.currentSize -= oldestElement.size
	}

	c.elements.Put(key, &sizedElement[V]{
		value: value,
		size:  newEntrySize,
	})
	c.currentSize += newEntrySize
}

func (c *sizedLRU[K, V]) get(key K) (V, bool) {
	element, ok := c.elements.Get(key)
	if !ok {
		return utils.Zero[V](), false
	}

	c.elements.Put(key, element) // Mark [k] as MRU.
	return element.value, true
}

func (c *sizedLRU[K, _]) evict(key K) {
	if element, ok := c.elements.Get(key); ok {
		c.elements.Delete(key)
		c.currentSize -= element.size
	}
}

func (c *sizedLRU[K, V]) flush() {
	c.elements.Clear()
	c.currentSize = 0
}

func (c *sizedLRU[_, _]) len() int {
	return c.elements.Len()
}

func (c *sizedLRU[_, _]) portionFilled() float64 {
	return float64(c.currentSize) / float64(c.maxSize)
}
