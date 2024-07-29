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
	size      int
}

func NewDeduplicator[K comparable, V cache.Evictable[K]](size int) *Deduplicator[K, V] {
	if size <= 0 {
		size = 1
	}
	return &Deduplicator[K, V]{
		entryMap:  make(map[K]*linked.ListElement[V]),
		entryList: linked.NewList[V](),
		size:      size,
	}
}

func (d *Deduplicator[_, V]) Deduplicate(value V) V {
	d.lock.Lock()
	defer d.lock.Unlock()

	key := value.Key()
	if e, ok := d.entryMap[key]; !ok {
		if d.entryList.Len() >= d.size {
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

func (d *Deduplicator[_, _]) Flush() {
	d.lock.Lock()
	defer d.lock.Unlock()

	for d.entryList.Len() > 0 {
		e := d.entryList.Front()
		d.entryList.Remove(e)

		delete(d.entryMap, e.Value.Key())
		e.Value.Evict()
	}
}
