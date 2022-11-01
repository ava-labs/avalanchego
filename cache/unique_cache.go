// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"container/list"
	"sync"
)

var _ Deduplicator[struct{}, testEvictable] = (*EvictableLRU[struct{}, testEvictable])(nil)

// Only used for the type assertion above
type testEvictable struct{}

func (testEvictable) Key() struct{} {
	return struct{}{}
}
func (testEvictable) Evict() {}

// EvictableLRU is an LRU cache that notifies the objects when they are evicted.
type EvictableLRU[T comparable, _ Evictable[T]] struct {
	lock      sync.Mutex
	entryMap  map[T]*list.Element
	entryList *list.List
	Size      int
}

func (c *EvictableLRU[T, _]) Deduplicate(value Evictable[T]) Evictable[T] {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.deduplicate(value)
}

func (c *EvictableLRU[_, _]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *EvictableLRU[T, _]) init() {
	if c.entryMap == nil {
		c.entryMap = make(map[T]*list.Element)
	}
	if c.entryList == nil {
		c.entryList = list.New()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
}

func (c *EvictableLRU[T, _]) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		val := e.Value.(Evictable[T])
		delete(c.entryMap, val.Key())
		val.Evict()
	}
}

func (c *EvictableLRU[T, _]) deduplicate(value Evictable[T]) Evictable[T] {
	c.init()
	c.resize()

	key := value.Key()
	if e, ok := c.entryMap[key]; !ok {
		if c.entryList.Len() >= c.Size {
			e = c.entryList.Front()
			c.entryList.MoveToBack(e)

			val := e.Value.(Evictable[T])
			delete(c.entryMap, val.Key())
			val.Evict()

			e.Value = value
		} else {
			e = c.entryList.PushBack(value)
		}
		c.entryMap[key] = e
	} else {
		c.entryList.MoveToBack(e)

		val := e.Value.(Evictable[T])
		value = val
	}
	return value
}

func (c *EvictableLRU[_, _]) flush() {
	c.init()

	size := c.Size
	c.Size = 0
	c.resize()
	c.Size = size
}
