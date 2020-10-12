// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

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

// LRU is a key value store with bounded size. If the size is attempted to be
// exceeded, then an element is removed from the cache before the insertion is
// done, based on evicting the least recently used value.
type LRU struct {
	lock      sync.Mutex
	entryMap  map[[32]byte]*list.Element
	entryList *list.List
	Size      int
}

// Put implements the cache interface
func (c *LRU) Put(key ids.ID, value interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

// Get implements the cache interface
func (c *LRU) Get(key ids.ID) (interface{}, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

// Evict implements the cache interface
func (c *LRU) Evict(key ids.ID) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

// Flush implements the cache interface
func (c *LRU) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *LRU) init() {
	if c.entryMap == nil {
		c.entryMap = make(map[[32]byte]*list.Element, minCacheSize)
	}
	if c.entryList == nil {
		c.entryList = list.New()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
}

func (c *LRU) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		val := e.Value.(*entry)
		delete(c.entryMap, val.Key.Key())
	}
}

func (c *LRU) put(key ids.ID, value interface{}) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key.Key()]; !ok {
		if c.entryList.Len() >= c.Size {
			e = c.entryList.Front()
			c.entryList.MoveToBack(e)

			val := e.Value.(*entry)
			delete(c.entryMap, val.Key.Key())
			val.Key = key
			val.Value = value
		} else {
			e = c.entryList.PushBack(&entry{
				Key:   key,
				Value: value,
			})
		}
		c.entryMap[key.Key()] = e
	} else {
		c.entryList.MoveToBack(e)

		val := e.Value.(*entry)
		val.Value = value
	}
}

func (c *LRU) get(key ids.ID) (interface{}, bool) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key.Key()]; ok {
		c.entryList.MoveToBack(e)

		val := e.Value.(*entry)
		return val.Value, true
	}
	return struct{}{}, false
}

func (c *LRU) evict(key ids.ID) {
	c.init()
	c.resize()

	keyBytes := key.Key()
	if e, ok := c.entryMap[keyBytes]; ok {
		c.entryList.Remove(e)
		delete(c.entryMap, keyBytes)
	}
}

func (c *LRU) flush() {
	c.init()

	c.entryMap = make(map[[32]byte]*list.Element, minCacheSize)
	c.entryList = list.New()
}
