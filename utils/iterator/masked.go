// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

var _ Iterator[any] = (*masked[any])(nil)

type masked[T any] struct {
	parentIterator Iterator[T]
	filter         func(T) bool
}

// NewMasked returns a new iterator that skips the elements in [parentIterator]
// that if the element is in [filter].
func NewMasked[T any](parentIterator Iterator[T], filter func(T) bool) Iterator[T] {
	return &masked[T]{
		parentIterator: parentIterator,
		filter:         filter,
	}
}

func (i *masked[T]) Next() bool {
	for i.parentIterator.Next() {
		element := i.parentIterator.Value()
		if !i.filter(element) {
			return true
		}
	}
	return false
}

func (i *masked[T]) Value() T {
	return i.parentIterator.Value()
}

func (i *masked[T]) Release() {
	i.parentIterator.Release()
}
