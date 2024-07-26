// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"sync"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils/linked"
)

var _ cache.Deduplicator[struct{}, cache.Evictable[struct{}]] = (*Deduplicator[struct{}, cache.Evictable[struct{}]])(nil)

// TODO: Remove this once the vertex state no longer uses it.
//
// Deduplicator is an LRU cache that notifies the objects when they are evicted.
type Deduplicator[K comparable, V cache.Evictable[K]] struct {
	lock      sync.Mutex
	entryMap  map[K]*linked.ListElement[V]
	entryList *linked.List[V]
	Size      int
}

func (d *Deduplicator[_, V]) Deduplicate(value V) V {
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.deduplicate(value)
}

func (d *Deduplicator[_, _]) Flush() {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.flush()
}

func (d *Deduplicator[K, V]) init() {
	if d.entryMap == nil {
		d.entryMap = make(map[K]*linked.ListElement[V])
	}
	if d.entryList == nil {
		d.entryList = linked.NewList[V]()
	}
	if d.Size <= 0 {
		d.Size = 1
	}
}

func (c *Deduplicator[_, V]) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		delete(c.entryMap, e.Value.Key())
		e.Value.Evict()
	}
}

func (d *Deduplicator[_, V]) deduplicate(value V) V {
	d.init()
	d.resize()

	key := value.Key()
	if e, ok := d.entryMap[key]; !ok {
		if d.entryList.Len() >= d.Size {
			e = d.entryList.Front()
			d.entryList.MoveToBack(e)

			delete(d.entryMap, e.Value.Key())
			e.Value.Evict()

			e.Value = value
		} else {
			e = &linked.ListElement[V]{
				Value: value,
			}
			d.entryList.PushBack(e)
		}
		d.entryMap[key] = e
	} else {
		d.entryList.MoveToBack(e)

		value = e.Value
	}
	return value
}

func (d *Deduplicator[_, _]) flush() {
	d.init()

	size := d.Size
	d.Size = 0
	d.resize()
	d.Size = size
}
