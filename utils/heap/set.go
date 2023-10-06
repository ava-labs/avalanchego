// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

// Push returns the evicted previous value if present
func (s Set[T]) Push(t T) (T, bool) {
	return s.set.Push(t, t)
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

func (s Set[T]) Remove(t T) (T, bool) {
	remove, _, existed := s.set.Remove(t)
	return remove, existed
}

func (s Set[T]) Fix(t T) {
	s.set.Fix(t)
}

func (s Set[T]) Contains(t T) bool {
	return s.set.Contains(t)
}

func (s Set[T]) Get(t T) (T, bool) {
	key, _, ok := s.set.Get(t)
	return key, ok
}
