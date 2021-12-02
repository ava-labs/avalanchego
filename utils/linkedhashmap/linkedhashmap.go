// Copyright (C) 2019-2021, Ava Labs, Inte. All rights reserved.
// See the file LICENSE for licensing terms.

package linkedhashmap

import (
	"container/list"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

// Hashmap provides an O(1) mapping from a comparable key to any value.
// Comparable is defined by https://golang.org/ref/spec#Comparison_operators.
type Hashmap interface {
	Put(key, val interface{})
	Get(key interface{}) (val interface{}, exists bool)
	Delete(key interface{})
	Len() int
}

// LinkedHashmap is a hashmap that keeps track of the oldest pairing an the
// newest pairing.
type LinkedHashmap interface {
	Hashmap

	Oldest() (key interface{}, val interface{}, exists bool)
	Newest() (key interface{}, val interface{}, exists bool)
	NewIterator() Iter
}

// Iterates over the keys and values in a LinkedHashmap
// from oldest to newest elements.
// Assumes the underlying LinkedHashmap is not modified while
// the iterator is in use, except to delete elements that
// have already been iterated over.
type Iter interface {
	Next() bool
	Key() interface{}
	Value() interface{}
}

type keyValue struct {
	key   interface{}
	value interface{}
}

type linkedHashmap struct {
	lock      sync.RWMutex
	entryMap  map[interface{}]*list.Element
	entryList *list.List
}

func New() LinkedHashmap {
	return &linkedHashmap{
		entryMap:  make(map[interface{}]*list.Element),
		entryList: list.New(),
	}
}

func (lh *linkedHashmap) Put(key, val interface{}) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	lh.put(key, val)
}

func (lh *linkedHashmap) Get(key interface{}) (interface{}, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.get(key)
}

func (lh *linkedHashmap) Delete(key interface{}) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	lh.delete(key)
}

func (lh *linkedHashmap) Len() int {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.len()
}

func (lh *linkedHashmap) Oldest() (interface{}, interface{}, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.oldest()
}

func (lh *linkedHashmap) Newest() (interface{}, interface{}, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.newest()
}

func (lh *linkedHashmap) put(key, value interface{}) {
	if e, ok := lh.entryMap[key]; ok {
		lh.entryList.MoveToBack(e)
		e.Value = keyValue{
			key:   key,
			value: value,
		}
	} else {
		lh.entryMap[key] = lh.entryList.PushBack(keyValue{
			key:   key,
			value: value,
		})
	}
}

func (lh *linkedHashmap) get(key interface{}) (interface{}, bool) {
	if e, ok := lh.entryMap[key]; ok {
		return e.Value.(keyValue).value, true
	}
	return nil, false
}

func (lh *linkedHashmap) delete(key interface{}) {
	if e, ok := lh.entryMap[key]; ok {
		lh.entryList.Remove(e)
		delete(lh.entryMap, key)
	}
}

func (lh *linkedHashmap) len() int { return len(lh.entryMap) }

func (lh *linkedHashmap) oldest() (interface{}, interface{}, bool) {
	if val := lh.entryList.Front(); val != nil {
		return val.Value.(keyValue).key, val.Value.(keyValue).value, true
	}
	return nil, nil, false
}

func (lh *linkedHashmap) newest() (interface{}, interface{}, bool) {
	if val := lh.entryList.Back(); val != nil {
		return val.Value.(keyValue).key, val.Value.(keyValue).value, true
	}
	return nil, nil, false
}

func (lh *linkedHashmap) NewIterator() Iter {
	return &iterator{lh: lh}
}

type iterator struct {
	lh                     *linkedHashmap
	key                    interface{}
	value                  interface{}
	next                   *list.Element
	initialized, exhausted bool
}

func (it *iterator) Next() bool {
	// If the iterator has been exhausted, there is no next value.
	if it.exhausted {
		it.key = ids.Empty
		it.value = nil
		it.next = nil
		return false
	}

	it.lh.lock.RLock()
	defer it.lh.lock.RUnlock()

	// If the iterator was not yet initialized, do it now.
	if !it.initialized {
		it.initialized = true
		oldest := it.lh.entryList.Front()
		if oldest == nil {
			it.exhausted = true
			it.key = ids.Empty
			it.value = nil
			it.next = nil
			return false
		}
		it.next = oldest
	}

	// It's important to ensure that [it.next] is not nil
	// by not deleting elements that have not yet been iterated
	// over from [it.lh]
	it.key = it.next.Value.(keyValue).key
	it.value = it.next.Value.(keyValue).value
	it.next = it.next.Next() // Next time, return next element
	it.exhausted = it.next == nil
	return true
}

func (it *iterator) Key() interface{}   { return it.key }
func (it *iterator) Value() interface{} { return it.value }
