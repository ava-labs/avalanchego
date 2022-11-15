// Copyright (C) 2019-2022, Ava Labs, Inte. All rights reserved.
// See the file LICENSE for licensing terms.

package linkedhashmap

import (
	"container/list"
	"sync"

	"github.com/ava-labs/avalanchego/utils"
)

var _ LinkedHashmap[int, struct{}] = (*linkedHashmap[int, struct{}])(nil)

// Hashmap provides an O(1) mapping from a comparable key to any value.
// Comparable is defined by https://golang.org/ref/spec#Comparison_operators.
type Hashmap[K, V any] interface {
	Put(key K, val V)
	Get(key K) (val V, exists bool)
	Delete(key K)
	Len() int
}

// LinkedHashmap is a hashmap that keeps track of the oldest pairing an the
// newest pairing.
type LinkedHashmap[K, V any] interface {
	Hashmap[K, V]

	Oldest() (key K, val V, exists bool)
	Newest() (key K, val V, exists bool)
	NewIterator() Iter[K, V]
}

type keyValue[K, V any] struct {
	key   K
	value V
}

type linkedHashmap[K comparable, V any] struct {
	lock      sync.RWMutex
	entryMap  map[K]*list.Element
	entryList *list.List
}

func New[K comparable, V any]() LinkedHashmap[K, V] {
	return &linkedHashmap[K, V]{
		entryMap:  make(map[K]*list.Element),
		entryList: list.New(),
	}
}

func (lh *linkedHashmap[K, V]) Put(key K, val V) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	lh.put(key, val)
}

func (lh *linkedHashmap[K, V]) Get(key K) (V, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.get(key)
}

func (lh *linkedHashmap[K, V]) Delete(key K) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	lh.delete(key)
}

func (lh *linkedHashmap[K, V]) Len() int {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.len()
}

func (lh *linkedHashmap[K, V]) Oldest() (K, V, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.oldest()
}

func (lh *linkedHashmap[K, V]) Newest() (K, V, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.newest()
}

func (lh *linkedHashmap[K, V]) put(key K, value V) {
	if e, ok := lh.entryMap[key]; ok {
		lh.entryList.MoveToBack(e)
		e.Value = keyValue[K, V]{
			key:   key,
			value: value,
		}
	} else {
		lh.entryMap[key] = lh.entryList.PushBack(keyValue[K, V]{
			key:   key,
			value: value,
		})
	}
}

func (lh *linkedHashmap[K, V]) get(key K) (V, bool) {
	if e, ok := lh.entryMap[key]; ok {
		return e.Value.(keyValue[K, V]).value, true
	}
	return utils.Zero[V](), false
}

func (lh *linkedHashmap[K, V]) delete(key K) {
	if e, ok := lh.entryMap[key]; ok {
		lh.entryList.Remove(e)
		delete(lh.entryMap, key)
	}
}

func (lh *linkedHashmap[K, V]) len() int {
	return len(lh.entryMap)
}

func (lh *linkedHashmap[K, V]) oldest() (K, V, bool) {
	if val := lh.entryList.Front(); val != nil {
		return val.Value.(keyValue[K, V]).key, val.Value.(keyValue[K, V]).value, true
	}
	return utils.Zero[K](), utils.Zero[V](), false
}

func (lh *linkedHashmap[K, V]) newest() (K, V, bool) {
	if val := lh.entryList.Back(); val != nil {
		return val.Value.(keyValue[K, V]).key, val.Value.(keyValue[K, V]).value, true
	}
	return utils.Zero[K](), utils.Zero[V](), false
}

func (lh *linkedHashmap[K, V]) NewIterator() Iter[K, V] {
	return &iterator[K, V]{lh: lh}
}
