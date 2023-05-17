// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
)

var _ Cacher[struct{}, SizedElement] = (*sizedLRU[struct{}, SizedElement])(nil)

type SizedElement interface {
	Size() int
}

// sizedLRU is a key value store with bounded size. If the size is attempted to
// be exceeded, then elements are removed from the cache until the bound is
// honored, based on evicting the least recently used value.
type sizedLRU[K comparable, V SizedElement] struct {
	lock        sync.Mutex
	elements    linkedhashmap.LinkedHashmap[K, V]
	maxSize     int
	currentSize int
}

func NewSizedLRU[K comparable, V SizedElement](maxSize int) Cacher[K, V] {
	return &sizedLRU[K, V]{
		elements: linkedhashmap.New[K, V](),
		maxSize:  maxSize,
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

func (c *sizedLRU[K, V]) put(key K, value V) {
	if oldValue, ok := c.elements.Get(key); ok {
		c.currentSize -= oldValue.Size()
	}

	// Remove elements until the size of elements in the cache <= [c.maxSize].
	valueSize := value.Size()
	for c.currentSize+valueSize > c.maxSize {
		oldestKey, value, _ := c.elements.Oldest()
		c.elements.Delete(oldestKey)
		c.currentSize -= value.Size()
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
		c.currentSize -= value.Size()
	}
}

func (c *sizedLRU[K, V]) flush() {
	c.elements = linkedhashmap.New[K, V]()
	c.currentSize = 0
}
