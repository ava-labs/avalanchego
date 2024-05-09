// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import "github.com/ava-labs/avalanchego/utils"

// Slice is a set of elements that supports indexing.
type Slice[T comparable] struct {
	// Indices maps the element in the set to the index that it appears in
	// Elements.
	//
	// Indices must not be modified externally.
	Indices map[T]int
	// Elements must not be modified externally.
	Elements []T
}

// Return a new indexable set with initial capacity [size].
// More or less than [size] elements can be added to this set.
func NewSlice[T comparable](size int) *Slice[T] {
	return &Slice[T]{
		Indices:  make(map[T]int, size),
		Elements: make([]T, 0, size),
	}
}

// Add the element to this set.
// If the element is already in the set, nothing happens.
func (s *Slice[T]) Add(e T) {
	_, ok := s.Indices[e]
	if ok {
		return
	}

	s.Indices[e] = len(s.Elements)
	s.Elements = append(s.Elements, e)
}

// Contains returns true iff the set contains this element.
func (s *Slice[T]) Contains(e T) bool {
	_, contains := s.Indices[e]
	return contains
}

// Remove the element from this set.
// If an element isn't in the set, it's ignored.
func (s *Slice[T]) Remove(e T) {
	indexToRemove, ok := s.Indices[e]
	if !ok {
		return
	}

	lastIndex := len(s.Elements) - 1
	if indexToRemove != lastIndex {
		lastElement := s.Elements[lastIndex]

		s.Indices[lastElement] = indexToRemove
		s.Elements[indexToRemove] = lastElement
	}

	delete(s.Indices, e)
	s.Elements[lastIndex] = utils.Zero[T]()
	s.Elements = s.Elements[:lastIndex]
}
