// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package heap

// NewSet returns a heap without duplicates ordered by its values
func NewSet[T comparable](less func(a, b T) bool) Set[T] {
	return Set[T]{
		set: NewMap[T, T](less),
	}
}

type Set[T comparable] struct {
	set Map[T, T]
}

// Push returns if the entry was added
func (s Set[T]) Push(t T) bool {
	_, hadValue := s.set.Push(t, t)
	return !hadValue
}

func (s Set[T]) Pop() (T, bool) {
	pop, _, ok := s.set.Pop()
	return pop, ok
}

func (s Set[T]) Peek() (T, bool) {
	peek, _, ok := s.set.Peek()
	return peek, ok
}

func (s Set[T]) Len() int {
	return s.set.Len()
}

func (s Set[T]) Remove(t T) bool {
	_, existed := s.set.Remove(t)
	return existed
}

func (s Set[T]) Fix(t T) {
	s.set.Fix(t)
}

func (s Set[T]) Contains(t T) bool {
	return s.set.Contains(t)
}
