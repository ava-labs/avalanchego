// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"container/list"
	"sync"
)

const minCacheSize = 32

// TODO can we do a type assertion with generics?
// var _ Cacher = &LRU{}

type entry[T comparable, K any] struct {
	Key   T
	Value K
}

// LRU is a key value store with bounded size. If the size is attempted to be
// exceeded, then an element is removed from the cache before the insertion is
// done, based on evicting the least recently used value.
type LRU[T comparable, K any] struct {
	lock      sync.Mutex
	entryMap  map[T]*list.Element
	entryList *list.List
	Size      int
}

func (c *LRU[T, K]) Put(key T, value K) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.put(key, value)
}

func (c *LRU[T, K]) Get(key T) (K, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.get(key)
}

func (c *LRU[T, _]) Evict(key T) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.evict(key)
}

func (c *LRU[T, _]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.flush()
}

func (c *LRU[T, _]) init() {
	if c.entryMap == nil {
		c.entryMap = make(map[T]*list.Element, minCacheSize)
	}
	if c.entryList == nil {
		c.entryList = list.New()
	}
	if c.Size <= 0 {
		c.Size = 1
	}
}

func (c *LRU[T, K]) resize() {
	for c.entryList.Len() > c.Size {
		e := c.entryList.Front()
		c.entryList.Remove(e)

		val := e.Value.(*entry[T, K])
		delete(c.entryMap, val.Key)
	}
}

func (c *LRU[T, K]) put(key T, value K) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key]; !ok {
		if c.entryList.Len() >= c.Size {
			e = c.entryList.Front()
			c.entryList.MoveToBack(e)

			val := e.Value.(*entry[T, K])
			delete(c.entryMap, val.Key)
			val.Key = key
			val.Value = value
		} else {
			e = c.entryList.PushBack(&entry[T, K]{
				Key:   key,
				Value: value,
			})
		}
		c.entryMap[key] = e
	} else {
		c.entryList.MoveToBack(e)

		val := e.Value.(*entry[T, K])
		val.Value = value
	}
}

func (c *LRU[T, K]) get(key T) (K, bool) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key]; ok {
		c.entryList.MoveToBack(e)

		val := e.Value.(*entry[T, K])
		return val.Value, true
	}
	return *new(K), false //nolint:gocritic
}

func (c *LRU[T, _]) evict(key T) {
	c.init()
	c.resize()

	if e, ok := c.entryMap[key]; ok {
		c.entryList.Remove(e)
		delete(c.entryMap, key)
	}
}

func (c *LRU[T, _]) flush() {
	c.init()

	c.entryMap = make(map[T]*list.Element, minCacheSize)
	c.entryList = list.New()
}
