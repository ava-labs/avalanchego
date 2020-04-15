// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"container/list"
	"sync"

	"github.com/ava-labs/gecko/ids"
)

// Closer ...
type Closer interface {
	Close()
}

// ClosableEntry is an entry in a LRUCloser cache
type ClosableEntry struct {
	Key   ids.ID
	Value Closer
}

// LRUCloser is an LRU cache where each value has a method Close(),
// which is called when the value is evicted from the cache
type LRUCloser struct {
	lock      sync.Mutex
	entryMap  map[[32]byte]*list.Element
	entryList *list.List
	Size      int
}

// Put implements the cache interface
func (c *LRUCloser) Put(key ids.ID, value Closer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

// Get implements the cache interface
func (c *LRUCloser) Get(key ids.ID) (Closer, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

// Evict implements the cache interface
func (c *LRUCloser) Evict(key ids.ID) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

// Flush implements the cache interface
func (c *LRUCloser) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *LRUCloser) init() {
	if c.entryMap == nil {
		c.entryMap = make(map[[32]byte]*list.Element)
	}
	if c.entryList == nil {
		c.entryList = list.New()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
}

func (c *LRUCloser) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		val := e.Value.(*ClosableEntry)
		val.Value.Close()
		delete(c.entryMap, val.Key.Key())
	}
}

func (c *LRUCloser) put(key ids.ID, value Closer) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key.Key()]; !ok { // this key isn't already in the cache
		if c.entryList.Len() >= c.Size {
			e = c.entryList.Front()
			c.entryList.MoveToBack(e)

			val := e.Value.(*ClosableEntry)
			val.Value.Close() // call close on the evicted value
			delete(c.entryMap, val.Key.Key())
			val.Key = key
			val.Value = value
		} else {
			e = c.entryList.PushBack(&ClosableEntry{
				Key:   key,
				Value: value,
			})
		}
		c.entryMap[key.Key()] = e
	} else {
		c.entryList.MoveToBack(e)

		val := e.Value.(*ClosableEntry)
		val.Value = value
	}
}

func (c *LRUCloser) get(key ids.ID) (Closer, bool) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key.Key()]; ok {
		c.entryList.MoveToBack(e)

		val := e.Value.(*ClosableEntry)
		return val.Value, true
	}
	return nil, false
}

func (c *LRUCloser) evict(key ids.ID) {
	c.init()
	c.resize()

	keyBytes := key.Key()
	if e, ok := c.entryMap[keyBytes]; ok {
		e.Value.(*ClosableEntry).Value.Close()
		c.entryList.Remove(e)
		delete(c.entryMap, keyBytes)
	}
}

func (c *LRUCloser) flush() {
	c.init()

	for _, elt := range c.entryMap {
		elt.Value.(*ClosableEntry).Value.Close()
	}

	c.entryMap = make(map[[32]byte]*list.Element)
	c.entryList = list.New()
}
