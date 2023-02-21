// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"container/list"
	"sync"

	"github.com/ava-labs/avalanchego/utils"
)

const minCacheSize = 32

var _ Cacher[struct{}, struct{}] = (*LRU[struct{}, struct{}])(nil)

type entry[K comparable, V any] struct {
	Key   K
	Value V
}

// LRU is a key value store with bounded size. If the size is attempted to be
// exceeded, then an element is removed from the cache before the insertion is
// done, based on evicting the least recently used value.
type LRU[K comparable, V any] struct {
	lock      sync.Mutex
	entryMap  map[K]*list.Element
	entryList *list.List
	Size      int
	// OnEviction is called with an internal lock held, and therefore should
	// never call any methods on the cache internally.
	OnEviction func(V)
}

func (c *LRU[K, V]) Put(key K, value V) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

func (c *LRU[K, _]) Evict(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

func (c *LRU[_, _]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *LRU[K, _]) init() {
	if c.entryMap == nil {
		c.entryMap = make(map[K]*list.Element, minCacheSize)
	}
	if c.entryList == nil {
		c.entryList = list.New()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
}

func (c *LRU[K, V]) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		val := e.Value.(*entry[K, V])
		delete(c.entryMap, val.Key)
		if c.OnEviction != nil {
			c.OnEviction(val.Value)
		}
	}
}

func (c *LRU[K, V]) put(key K, value V) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key]; !ok {
		if c.entryList.Len() >= c.Size {
			e = c.entryList.Front()
			c.entryList.MoveToBack(e)

			val := e.Value.(*entry[K, V])
			delete(c.entryMap, val.Key)
			if c.OnEviction != nil {
				c.OnEviction(val.Value)
			}
			val.Key = key
			val.Value = value
		} else {
			e = c.entryList.PushBack(&entry[K, V]{
				Key:   key,
				Value: value,
			})
		}
		c.entryMap[key] = e
	} else {
		c.entryList.MoveToBack(e)

		val := e.Value.(*entry[K, V])
		val.Value = value
	}
}

func (c *LRU[K, V]) get(key K) (V, bool) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key]; ok {
		c.entryList.MoveToBack(e)

		val := e.Value.(*entry[K, V])
		return val.Value, true
	}
	return utils.Zero[V](), false
}

func (c *LRU[K, V]) evict(key K) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key]; ok {
		c.entryList.Remove(e)
		delete(c.entryMap, key)

		if c.OnEviction != nil {
			val := e.Value.(*entry[K, V])
			c.OnEviction(val.Value)
		}
	}
}

func (c *LRU[K, V]) flush() {
	c.init()

	if c.OnEviction != nil {
		for _, v := range c.entryMap {
			val := v.Value.(*entry[K, V])
			c.OnEviction(val.Value)
		}
	}

	c.entryMap = make(map[K]*list.Element, minCacheSize)
	c.entryList = list.New()
}
