// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"bytes"
	"encoding/json"
	"slices"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

// The minimum capacity of a set
const minSetSize = 16

var _ json.Marshaler = (*Set[int])(nil)

// Set is a set of elements.
type Set[T comparable] map[T]struct{}

// Of returns a Set initialized with [elts]
func Of[T comparable](elts ...T) Set[T] {
	s := NewSet[T](len(elts))
	s.Add(elts...)
	return s
}

// Return a new set with initial capacity [size].
// More or less than [size] elements can be added to this set.
// Using NewSet() rather than Set[T]{} is just an optimization that can
// be used if you know how many elements will be put in this set.
func NewSet[T comparable](size int) Set[T] {
	if size < 0 {
		return Set[T]{}
	}
	return make(map[T]struct{}, size)
}

func (s *Set[T]) resize(size int) {
	if *s == nil {
		if minSetSize > size {
			size = minSetSize
		}
		*s = make(map[T]struct{}, size)
	}
}

// Add all the elements to this set.
// If the element is already in the set, nothing happens.
func (s *Set[T]) Add(elts ...T) {
	s.resize(2 * len(elts))
	for _, elt := range elts {
		(*s)[elt] = struct{}{}
	}
}

// Union adds all the elements from the provided set to this set.
func (s *Set[T]) Union(set Set[T]) {
	s.resize(2 * set.Len())
	for elt := range set {
		(*s)[elt] = struct{}{}
	}
}

// Difference removes all the elements in [set] from [s].
func (s *Set[T]) Difference(set Set[T]) {
	for elt := range set {
		delete(*s, elt)
	}
}

// Contains returns true iff the set contains this element.
func (s *Set[T]) Contains(elt T) bool {
	_, contains := (*s)[elt]
	return contains
}

// Overlaps returns true if the intersection of the set is non-empty
func (s *Set[T]) Overlaps(big Set[T]) bool {
	small := *s
	if small.Len() > big.Len() {
		small, big = big, small
	}

	for elt := range small {
		if _, ok := big[elt]; ok {
			return true
		}
	}
	return false
}

// Len returns the number of elements in this set.
func (s Set[_]) Len() int {
	return len(s)
}

// Remove all the given elements from this set.
// If an element isn't in the set, it's ignored.
func (s *Set[T]) Remove(elts ...T) {
	for _, elt := range elts {
		delete(*s, elt)
	}
}

// Clear empties this set
func (s *Set[_]) Clear() {
	clear(*s)
}

// List converts this set into a list
func (s Set[T]) List() []T {
	return maps.Keys(s)
}

// Equals returns true if the sets contain the same elements
func (s Set[T]) Equals(other Set[T]) bool {
	return maps.Equal(s, other)
}

// Removes and returns an element.
// If the set is empty, does nothing and returns false.
func (s *Set[T]) Pop() (T, bool) {
	for elt := range *s {
		delete(*s, elt)
		return elt, true
	}
	return utils.Zero[T](), false
}

func (s *Set[T]) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == avajson.Null {
		return nil
	}
	var elts []T
	if err := json.Unmarshal(b, &elts); err != nil {
		return err
	}
	s.Clear()
	s.Add(elts...)
	return nil
}

func (s Set[_]) MarshalJSON() ([]byte, error) {
	var (
		eltBytes = make([][]byte, len(s))
		i        int
		err      error
	)
	for elt := range s {
		eltBytes[i], err = json.Marshal(elt)
		if err != nil {
			return nil, err
		}
		i++
	}
	// Sort for determinism
	slices.SortFunc(eltBytes, bytes.Compare)

	// Build the JSON
	var (
		jsonBuf = bytes.Buffer{}
		errs    = wrappers.Errs{}
	)
	_, err = jsonBuf.WriteString("[")
	errs.Add(err)
	for i, elt := range eltBytes {
		_, err := jsonBuf.Write(elt)
		errs.Add(err)
		if i != len(eltBytes)-1 {
			_, err := jsonBuf.WriteString(",")
			errs.Add(err)
		}
	}
	_, err = jsonBuf.WriteString("]")
	errs.Add(err)

	return jsonBuf.Bytes(), errs.Err
}

// Returns a random element. If the set is empty, returns false
func (s *Set[T]) Peek() (T, bool) {
	for elt := range *s {
		return elt, true
	}
	return utils.Zero[T](), false
}

// Intersect returns the set intersection of s1 and s2
func Intersect[T comparable](s1 Set[T], s2 Set[T]) Set[T] {
	s := Set[T]{}
	for k := range s1 {
		if !s2.Contains(k) {
			continue
		}

		s.Add(k)
	}

	return s
}
