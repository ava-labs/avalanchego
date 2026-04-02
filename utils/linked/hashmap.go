// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linked

import "github.com/ava-labs/avalanchego/utils"

type keyValue[K, V any] struct {
	key   K
	value V
}

// Hashmap provides an ordered O(1) mapping from keys to values.
//
// Entries are tracked by insertion order.
type Hashmap[K comparable, V any] struct {
	entryMap  map[K]*ListElement[keyValue[K, V]]
	entryList *List[keyValue[K, V]]
	freeList  []*ListElement[keyValue[K, V]]
}

func NewHashmap[K comparable, V any]() *Hashmap[K, V] {
	return NewHashmapWithSize[K, V](0)
}

func NewHashmapWithSize[K comparable, V any](initialSize int) *Hashmap[K, V] {
	lh := &Hashmap[K, V]{
		entryMap:  make(map[K]*ListElement[keyValue[K, V]], initialSize),
		entryList: NewList[keyValue[K, V]](),
		freeList:  make([]*ListElement[keyValue[K, V]], initialSize),
	}
	for i := range lh.freeList {
		lh.freeList[i] = &ListElement[keyValue[K, V]]{}
	}
	return lh
}

func (lh *Hashmap[K, V]) Put(key K, value V) {
	if e, ok := lh.entryMap[key]; ok {
		lh.entryList.MoveToBack(e)
		e.Value = keyValue[K, V]{
			key:   key,
			value: value,
		}
		return
	}

	var e *ListElement[keyValue[K, V]]
	if numFree := len(lh.freeList); numFree > 0 {
		numFree--
		e = lh.freeList[numFree]
		lh.freeList = lh.freeList[:numFree]
	} else {
		e = &ListElement[keyValue[K, V]]{}
	}

	e.Value = keyValue[K, V]{
		key:   key,
		value: value,
	}
	lh.entryMap[key] = e
	lh.entryList.PushBack(e)
}

func (lh *Hashmap[K, V]) Get(key K) (V, bool) {
	if e, ok := lh.entryMap[key]; ok {
		return e.Value.value, true
	}
	return utils.Zero[V](), false
}

func (lh *Hashmap[K, V]) Delete(key K) bool {
	e, ok := lh.entryMap[key]
	if ok {
		lh.remove(e)
	}
	return ok
}

func (lh *Hashmap[K, V]) Clear() {
	for _, e := range lh.entryMap {
		lh.remove(e)
	}
}

// remove assumes that [e] is currently in the Hashmap.
func (lh *Hashmap[K, V]) remove(e *ListElement[keyValue[K, V]]) {
	delete(lh.entryMap, e.Value.key)
	lh.entryList.Remove(e)
	e.Value = keyValue[K, V]{} // Free the key value pair
	lh.freeList = append(lh.freeList, e)
}

func (lh *Hashmap[K, V]) Len() int {
	return len(lh.entryMap)
}

func (lh *Hashmap[K, V]) Oldest() (K, V, bool) {
	if e := lh.entryList.Front(); e != nil {
		return e.Value.key, e.Value.value, true
	}
	return utils.Zero[K](), utils.Zero[V](), false
}

func (lh *Hashmap[K, V]) Newest() (K, V, bool) {
	if e := lh.entryList.Back(); e != nil {
		return e.Value.key, e.Value.value, true
	}
	return utils.Zero[K](), utils.Zero[V](), false
}

func (lh *Hashmap[K, V]) NewIterator() *Iterator[K, V] {
	return &Iterator[K, V]{lh: lh}
}

// Iterates over the keys and values in a LinkedHashmap from oldest to newest.
// Assumes the underlying LinkedHashmap is not modified while the iterator is in
// use, except to delete elements that have already been iterated over.
type Iterator[K comparable, V any] struct {
	lh                     *Hashmap[K, V]
	key                    K
	value                  V
	next                   *ListElement[keyValue[K, V]]
	initialized, exhausted bool
}

func (it *Iterator[K, V]) Next() bool {
	// If the iterator has been exhausted, there is no next value.
	if it.exhausted {
		it.key = utils.Zero[K]()
		it.value = utils.Zero[V]()
		it.next = nil
		return false
	}

	// If the iterator was not yet initialized, do it now.
	if !it.initialized {
		it.initialized = true
		oldest := it.lh.entryList.Front()
		if oldest == nil {
			it.exhausted = true
			it.key = utils.Zero[K]()
			it.value = utils.Zero[V]()
			it.next = nil
			return false
		}
		it.next = oldest
	}

	// It's important to ensure that [it.next] is not nil
	// by not deleting elements that have not yet been iterated
	// over from [it.lh]
	it.key = it.next.Value.key
	it.value = it.next.Value.value
	it.next = it.next.Next() // Next time, return next element
	it.exhausted = it.next == nil
	return true
}

func (it *Iterator[K, V]) Key() K {
	return it.key
}

func (it *Iterator[K, V]) Value() V {
	return it.value
}
