// (c) 2019-2021, Ava Labs, Inte. All rights reserved.
// See the file LICENSE for licensing terms.

package linkedhashmap

import (
	"container/list"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

// Hashmap provides an O(1) mapping from an [ids.ID] to any value.
type Hashmap interface {
	Put(key ids.ID, val interface{})
	Get(key ids.ID) (val interface{}, exists bool)
	Delete(key ids.ID)
	Len() int
}

// LinkedHashmap is a hashmap that keeps track of the oldest pairing an the
// newest pairing.
type LinkedHashmap interface {
	Hashmap

	Oldest() (val interface{}, exists bool)
	Newest() (val interface{}, exists bool)
	NewIterator() Iter
}

// Iterates over the keys and values in a LinkedHashmap
// from oldest to newest elements.
// Assumes the underlying LinkedHashmap is not modified while
// the iterator is in use, except to delete elements that
// have already been iterated over.
// TODO is this the right spec?
type Iter interface {
	Next() bool
	Key() ids.ID
	Value() interface{}
}

type keyValue struct {
	key   ids.ID
	value interface{}
}

type linkedHashmap struct {
	lock      sync.RWMutex
	entryMap  map[ids.ID]*list.Element
	entryList *list.List
}

func New() LinkedHashmap {
	return &linkedHashmap{
		entryMap:  make(map[ids.ID]*list.Element),
		entryList: list.New(),
	}
}

func (lh *linkedHashmap) Put(key ids.ID, val interface{}) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	lh.put(key, val)
}

func (lh *linkedHashmap) Get(key ids.ID) (interface{}, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.get(key)
}

func (lh *linkedHashmap) Delete(key ids.ID) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	lh.delete(key)
}

func (lh *linkedHashmap) Len() int {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.len()
}

func (lh *linkedHashmap) Oldest() (interface{}, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.oldest()
}

func (lh *linkedHashmap) Newest() (interface{}, bool) {
	lh.lock.Lock()
	defer lh.lock.Unlock()

	return lh.newest()
}

func (lh *linkedHashmap) put(key ids.ID, value interface{}) {
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

func (lh *linkedHashmap) get(key ids.ID) (interface{}, bool) {
	if e, ok := lh.entryMap[key]; ok {
		return e.Value.(keyValue).value, true
	}
	return nil, false
}

func (lh *linkedHashmap) delete(key ids.ID) {
	if e, ok := lh.entryMap[key]; ok {
		lh.entryList.Remove(e)
		delete(lh.entryMap, key)
	}
}

func (lh *linkedHashmap) len() int { return len(lh.entryMap) }

func (lh *linkedHashmap) oldest() (interface{}, bool) {
	if val := lh.entryList.Front(); val != nil {
		return val.Value.(keyValue).value, true
	}
	return nil, false
}

func (lh *linkedHashmap) newest() (interface{}, bool) {
	if val := lh.entryList.Back(); val != nil {
		return val.Value.(keyValue).value, true
	}
	return nil, false
}

func (lh *linkedHashmap) NewIterator() Iter {
	return &iterator{lh: lh}
}

type iterator struct {
	lh                     *linkedHashmap
	key                    ids.ID
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

func (it *iterator) Key() ids.ID        { return it.key }
func (it *iterator) Value() interface{} { return it.value }
