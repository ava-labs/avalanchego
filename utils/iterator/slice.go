// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

var _ Iterator[any] = (*slice[any])(nil)

type slice[T any] struct {
	index    int
	elements []T
}

// FromSlice returns an iterator that contains [elements] in order. Doesn't sort
// by anything.
func FromSlice[T any](elements ...T) Iterator[T] {
	return &slice[T]{
		index:    -1,
		elements: elements,
	}
}

func (i *slice[_]) Next() bool {
	i.index++
	return i.index < len(i.elements)
}

func (i *slice[T]) Value() T {
	return i.elements[i.index]
}

func (*slice[_]) Release() {}
