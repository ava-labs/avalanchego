// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import "github.com/ava-labs/avalanchego/utils/set"

var _ Iterator[any] = (*deduplicator[any])(nil)

type deduplicator[T comparable] struct {
	it   Iterator[T]
	seen set.Set[T]
}

// Deduplicate returns an iterator that skips the elements that have already
// been returned from [it].
func Deduplicate[T comparable](it Iterator[T]) Iterator[T] {
	return &deduplicator[T]{
		it: it,
	}
}

func (i *deduplicator[_]) Next() bool {
	for i.it.Next() {
		element := i.it.Value()
		if !i.seen.Contains(element) {
			i.seen.Add(element)
			return true
		}
	}
	return false
}

func (i *deduplicator[T]) Value() T {
	return i.it.Value()
}

func (i *deduplicator[_]) Release() {
	i.it.Release()
}
