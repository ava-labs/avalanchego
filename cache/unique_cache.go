// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils/linked"
)

var _ Deduplicator[struct{}, Evictable[struct{}]] = (*EvictableLRU[struct{}, Evictable[struct{}]])(nil)

// EvictableLRU is an LRU cache that notifies the objects when they are evicted.
type EvictableLRU[K comparable, V Evictable[K]] struct {
	lock      sync.Mutex
	entryMap  map[K]*linked.ListElement[V]
	entryList *linked.List[V]
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

func (c *EvictableLRU[K, V]) init() {
	if c.entryMap == nil {
		c.entryMap = make(map[K]*linked.ListElement[V])
	}
	if c.entryList == nil {
		c.entryList = linked.NewList[V]()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
}

func (c *EvictableLRU[_, V]) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		delete(c.entryMap, e.Value.Key())
		e.Value.Evict()
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

			delete(c.entryMap, e.Value.Key())
			e.Value.Evict()

			e.Value = value
		} else {
			e = &linked.ListElement[V]{
				Value: value,
			}
			c.entryList.PushBack(e)
		}
		c.entryMap[key] = e
	} else {
		c.entryList.MoveToBack(e)

		value = e.Value
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
