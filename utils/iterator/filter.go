// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import "github.com/ava-labs/avalanchego/utils/set"

var _ Iterator[any] = (*filtered[any])(nil)

type filtered[T any] struct {
	it     Iterator[T]
	filter func(T) bool
}

// Filter returns an iterator that skips the elements in [it] that return true
// from [filter].
func Filter[T any](it Iterator[T], filter func(T) bool) Iterator[T] {
	return &filtered[T]{
		it:     it,
		filter: filter,
	}
}

// Deduplicate returns an iterator that skips the elements that have already
// been returned from [it].
func Deduplicate[T comparable](it Iterator[T]) Iterator[T] {
	var seen set.Set[T]
	return Filter(it, func(e T) bool {
		if seen.Contains(e) {
			return true
		}
		seen.Add(e)
		return false
	})
}

func (i *filtered[_]) Next() bool {
	for i.it.Next() {
		element := i.it.Value()
		if !i.filter(element) {
			return true
		}
	}
	return false
}

func (i *filtered[T]) Value() T {
	return i.it.Value()
}

func (i *filtered[_]) Release() {
	i.it.Release()
}
