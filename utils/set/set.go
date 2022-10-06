// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// The minimum capacity of a set
	minSetSize = 16

	// If a set has more than this many keys, it will be cleared by setting the map to nil
	// rather than iteratively deleting
	clearSizeThreshold = 512
)

// Settable describes an element that can be in a set.
type Settable interface {
	comparable
	fmt.Stringer
}

// Set is a set of elements.
type Set[T Settable] map[T]struct{}

// Return a new set with initial capacity [size].
// More or less than [size] elements can be added to this set.
// Using NewSet() rather than Set[T]{} is just an optimization that can
// be used if you know how many elements will be put in this set.
func NewSet[T Settable](size int) Set[T] {
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
func (s Set[_]) Len() int { return len(s) }

// Remove all the given elements from this set.
// If an element isn't in the set, it's ignored.
func (s *Set[T]) Remove(elts ...T) {
	for _, elt := range elts {
		delete(*s, elt)
	}
}

// Clear empties this set
func (s *Set[_]) Clear() {
	if len(*s) > clearSizeThreshold {
		*s = nil
		return
	}
	for elt := range *s {
		delete(*s, elt)
	}
}

// List converts this set into a list
func (s Set[T]) List() []T {
	elts := make([]T, s.Len())
	i := 0
	for elt := range s {
		elts[i] = elt
		i++
	}
	return elts
}

// CappedList returns a list of length at most [size].
// Size should be >= 0. If size < 0, returns nil.
func (s Set[T]) CappedList(size int) []T {
	if size < 0 {
		return nil
	}
	if l := s.Len(); l < size {
		size = l
	}
	i := 0
	elts := make([]T, size)
	for elt := range s {
		if i >= size {
			break
		}
		elts[i] = elt
		i++
	}
	return elts
}

// Equals returns true if the sets contain the same elements
func (s Set[T]) Equals(other Set[T]) bool {
	if s.Len() != other.Len() {
		return false
	}
	for elt := range other {
		if _, contains := s[elt]; !contains {
			return false
		}
	}
	return true
}

// String returns the string representation of a set
func (s Set[_]) String() string {
	sb := strings.Builder{}
	sb.WriteString("{")
	first := true
	for elt := range s {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(elt.String())
	}
	sb.WriteString("}")
	return sb.String()
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

func (s *Set[_]) MarshalJSON() ([]byte, error) {
	elts := s.List()

	// Sort for determinism
	asStrs := make([]string, len(elts))
	for i, elt := range elts {
		asStrs[i] = fmt.Sprintf("%v", elt)
	}
	sort.Strings(asStrs)

	// Build the JSON
	var (
		jsonStr = bytes.Buffer{}
		errs    = wrappers.Errs{}
	)
	_, err := jsonStr.WriteString("[")
	errs.Add(err)
	for i, str := range asStrs {
		_, err := jsonStr.WriteString("\"" + str + "\"")
		errs.Add(err)
		if i != len(asStrs)-1 {
			_, err := jsonStr.WriteString(",")
			errs.Add(err)
		}
	}
	_, err = jsonStr.WriteString("]")
	errs.Add(err)

	return jsonStr.Bytes(), errs.Err
}

// Returns an element. If the set is empty, returns false
func (s *Set[T]) Peek() (T, bool) {
	for elt := range *s {
		return elt, true
	}
	return *new(T), false //nolint:gocritic
}
