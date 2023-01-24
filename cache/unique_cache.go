// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"container/list"
	"sync"
)

var _ Deduplicator[struct{}, Evictable[struct{}]] = (*EvictableLRU[struct{}, Evictable[struct{}]])(nil)

// EvictableLRU is an LRU cache that notifies the objects when they are evicted.
type EvictableLRU[K comparable, _ Evictable[K]] struct {
	lock      sync.Mutex
	entryMap  map[K]*list.Element
	entryList *list.List
	Size      int
}

func (c *EvictableLRU[_, V]) Deduplicate(value V) V {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.deduplicate(value)
}

func (c *EvictableLRU[_, _]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *EvictableLRU[K, _]) init() {
	if c.entryMap == nil {
		c.entryMap = make(map[K]*list.Element)
	}
	if c.entryList == nil {
		c.entryList = list.New()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
}

func (c *EvictableLRU[_, V]) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		val := e.Value.(V)
		delete(c.entryMap, val.Key())
		val.Evict()
	}
}

func (c *EvictableLRU[_, V]) deduplicate(value V) V {
	c.init()
	c.resize()

	key := value.Key()
	if e, ok := c.entryMap[key]; !ok {
		if c.entryList.Len() >= c.Size {
			e = c.entryList.Front()
			c.entryList.MoveToBack(e)

			val := e.Value.(V)
			delete(c.entryMap, val.Key())
			val.Evict()

			e.Value = value
		} else {
			e = c.entryList.PushBack(value)
		}
		c.entryMap[key] = e
	} else {
		c.entryList.MoveToBack(e)

		val := e.Value.(V)
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
