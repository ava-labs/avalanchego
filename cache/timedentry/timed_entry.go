// (c) 2019-2021, Ava Labs, Inte. All rights reserved.
// See the file LICENSE for licensing terms.

package timedentry

import (
	"container/list"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	minCacheSize = 32
)

type entry struct {
	Key   ids.ID
	Value interface{}
}

// TimedEntries is a key value store with bounded size. If the size is attempted to be
// exceeded, then an element is removed from the cache before the insertion is
// done, based on evicting the least recently used value.
type TimedEntries struct {
	lock      sync.Mutex
	entryMap  map[ids.ID]*list.Element
	entryList *list.List
	Size      int
}

// Put implements the cache interface
func (te *TimedEntries) Put(key ids.ID, val interface{}) {
	te.lock.Lock()
	defer te.lock.Unlock()

	te.put(key, val)
}

// Get implements the cache interface
func (te *TimedEntries) Get(key ids.ID) (interface{}, bool) {
	te.lock.Lock()
	defer te.lock.Unlock()

	return te.get(key)
}

// Evict implements the cache interface
func (te *TimedEntries) Evict(key ids.ID) {
	te.lock.Lock()
	defer te.lock.Unlock()

	te.evict(key)
}

// Flush implements the cache interface
func (te *TimedEntries) Flush() {
	te.lock.Lock()
	defer te.lock.Unlock()

	te.flush()
}

func (te *TimedEntries) init() {
	if te.entryMap == nil {
		te.entryMap = make(map[ids.ID]*list.Element, minCacheSize)
	}
	if te.entryList == nil {
		te.entryList = list.New()
	}
	if te.Size <= 0 {
		te.Size = 1
	}
}

func (te *TimedEntries) resize() {
	for te.entryList.Len() > te.Size {
		e := te.entryList.Front()
		te.entryList.Remove(e)

		val := e.Value.(*entry)
		delete(te.entryMap, val.Key)
	}
}

func (te *TimedEntries) put(key ids.ID, value interface{}) {
	te.init()
	te.resize()

	if e, ok := te.entryMap[key]; !ok {
		if te.entryList.Len() >= te.Size {
			e = te.entryList.Front()
			te.entryList.MoveToBack(e)

			val := e.Value.(*entry)
			delete(te.entryMap, val.Key)
			val.Key = key
			val.Value = value
		} else {
			e = te.entryList.PushBack(&entry{
				Key:   key,
				Value: value,
			})
		}
		te.entryMap[key] = e
	} else {
		te.entryList.MoveToBack(e)

		val := e.Value.(*entry)
		val.Value = value
	}
}

func (te *TimedEntries) get(key ids.ID) (interface{}, bool) {
	te.init()
	te.resize()

	if e, ok := te.entryMap[key]; ok {
		// TODO should the get move the entry to the back ?
		// te.entryList.MoveToBack(e)

		val := e.Value.(*entry)
		return val.Value, true
	}
	return struct{}{}, false
}

func (te *TimedEntries) evict(key ids.ID) {
	te.init()
	te.resize()

	if e, ok := te.entryMap[key]; ok {
		te.entryList.Remove(e)
		delete(te.entryMap, key)
	}
}

func (te *TimedEntries) flush() {
	te.init()

	te.entryMap = make(map[ids.ID]*list.Element, minCacheSize)
	te.entryList = list.New()
}

func (te *TimedEntries) OldestRequest() interface{} {
	te.lock.Lock()
	defer te.lock.Unlock()

	if te.entryMap == nil || te.entryList == nil {
		return nil
	}
	if val := te.entryList.Front(); val != nil {
		return val.Value.(*entry).Value
	}

	return nil
}

func (te *TimedEntries) Len() int {
	te.lock.Lock()
	defer te.lock.Unlock()

	if te.entryMap == nil || te.entryList == nil {
		return 0
	}
	return te.entryList.Len()
}
