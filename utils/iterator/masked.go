// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import "github.com/ava-labs/avalanchego/ids"

var _ Iterator[Identifiable] = (*masked[Identifiable])(nil)

type masked[T Identifiable] struct {
	parentIterator Iterator[T]
	maskedElements map[ids.ID]T
}

// NewMasked returns a new iterator that skips the elements in [parentIterator]
// that are present in [maskedElements].
func NewMasked[T Identifiable](parentIterator Iterator[T], maskedElements map[ids.ID]T) Iterator[T] {
	return &masked[T]{
		parentIterator: parentIterator,
		maskedElements: maskedElements,
	}
}

func (i *masked[T]) Next() bool {
	for i.parentIterator.Next() {
		element := i.parentIterator.Value()
		if _, ok := i.maskedElements[element.ID()]; !ok {
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
