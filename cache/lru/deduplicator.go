// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils/linked"
)

// Evictable allows the object to be notified when it is evicted
//
// Deprecated: Remove this once the vertex state no longer uses it.
type Evictable[K comparable] interface {
	Key() K
	Evict()
}

// Deduplicator is an LRU cache that notifies the objects when they are evicted.
//
// Deprecated: Remove this once the vertex state no longer uses it.
type Deduplicator[K comparable, V Evictable[K]] struct {
	lock      sync.Mutex
	entryMap  map[K]*linked.ListElement[V]
	entryList *linked.List[V]
	size      int
}

// Deprecated: Remove this once the vertex state no longer uses it.
func NewDeduplicator[K comparable, V Evictable[K]](size int) *Deduplicator[K, V] {
	return &Deduplicator[K, V]{
		entryMap:  make(map[K]*linked.ListElement[V]),
		entryList: linked.NewList[V](),
		size:      max(size, 1),
	}
}

// Deduplicate returns either the provided value, or a previously provided value
// with the same ID that hasn't yet been evicted
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

// Flush removes all entries from the cache
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
