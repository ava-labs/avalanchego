// Copyright (C) 2019-2022, Ava Labs, Inte. All rights reserved.
// See the file LICENSE for licensing terms.

package linkedhashmap

import (
	"container/list"

	"github.com/ava-labs/avalanchego/utils"
)

var _ Iter[int, struct{}] = (*iterator[int, struct{}])(nil)

// Iterates over the keys and values in a LinkedHashmap
// from oldest to newest elements.
// Assumes the underlying LinkedHashmap is not modified while
// the iterator is in use, except to delete elements that
// have already been iterated over.
type Iter[K, V any] interface {
	Next() bool
	Key() K
	Value() V
}

type iterator[K comparable, V any] struct {
	lh                     *linkedHashmap[K, V]
	key                    K
	value                  V
	next                   *list.Element
	initialized, exhausted bool
}

func (it *iterator[K, V]) Next() bool {
	// If the iterator has been exhausted, there is no next value.
	if it.exhausted {
		it.key = utils.Zero[K]()
		it.value = utils.Zero[V]()
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
	it.key = it.next.Value.(keyValue[K, V]).key
	it.value = it.next.Value.(keyValue[K, V]).value
	it.next = it.next.Next() // Next time, return next element
	it.exhausted = it.next == nil
	return true
}

func (it *iterator[K, V]) Key() K {
	return it.key
}

func (it *iterator[K, V]) Value() V {
	return it.value
}
