// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"bytes"
	"sort"
	"strings"

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
	String() string
}

// Set is a set of IDs
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

func (ids *Set[T]) init(size int) {
	if *ids == nil {
		if minSetSize > size {
			size = minSetSize
		}
		*ids = make(map[T]struct{}, size)
	}
}

// Add all the ids to this set, if the id is already in the set, nothing happens
func (ids *Set[T]) Add(elts ...T) {
	ids.init(2 * len(elts))
	for _, id := range elts {
		(*ids)[id] = struct{}{}
	}
}

// Union adds all the ids from the provided set to this set.
func (ids *Set[T]) Union(set Set[T]) {
	ids.init(2 * set.Len())
	for id := range set {
		(*ids)[id] = struct{}{}
	}
}

// Difference removes all the ids from the provided set to this set.
func (ids *Set[T]) Difference(set Set[T]) {
	for elt := range set {
		delete(*ids, elt)
	}
}

// Contains returns true if the set contains this id, false otherwise
func (ids *Set[T]) Contains(elt T) bool {
	_, contains := (*ids)[elt]
	return contains
}

// Overlaps returns true if the intersection of the set is non-empty
func (ids *Set[T]) Overlaps(big Set[T]) bool {
	small := *ids
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

// Len returns the number of ids in this set
func (ids Set[_]) Len() int { return len(ids) }

// Remove all the id from this set, if the id isn't in the set, nothing happens
func (ids *Set[T]) Remove(elts ...T) {
	for _, elt := range elts {
		delete(*ids, elt)
	}
}

// Clear empties this set
func (ids *Set[_]) Clear() {
	if len(*ids) > clearSizeThreshold {
		*ids = nil
		return
	}
	for elt := range *ids {
		delete(*ids, elt)
	}
}

// List converts this set into a list
func (ids Set[T]) List() []T {
	idList := make([]T, ids.Len())
	i := 0
	for id := range ids {
		idList[i] = id
		i++
	}
	return idList
}

// CappedList returns a list of length at most [size].
// Size should be >= 0. If size < 0, returns nil.
func (ids Set[T]) CappedList(size int) []T {
	if size < 0 {
		return nil
	}
	if l := ids.Len(); l < size {
		size = l
	}
	i := 0
	idList := make([]T, size)
	for elt := range ids {
		if i >= size {
			break
		}
		idList[i] = elt
		i++
	}
	return idList
}

// Equals returns true if the sets contain the same elements
func (ids Set[T]) Equals(other Set[T]) bool {
	if ids.Len() != other.Len() {
		return false
	}
	for elt := range other {
		if _, contains := ids[elt]; !contains {
			return false
		}
	}
	return true
}

// String returns the string representation of a set
func (ids Set[_]) String() string {
	sb := strings.Builder{}
	sb.WriteString("{")
	first := true
	for id := range ids {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(id.String())
	}
	sb.WriteString("}")
	return sb.String()
}

// Removes and returns an element. If the set is empty, does nothing and returns
// false.
func (ids *Set[T]) Pop() (T, bool) {
	for id := range *ids {
		delete(*ids, id)
		return id, true
	}
	return *new(T), false //nolint:gocritic
}

func (ids *Set[_]) MarshalJSON() ([]byte, error) {
	idsList := ids.List()

	// Sort for determinism
	asStrs := make([]string, len(idsList))
	for i, id := range idsList {
		asStrs[i] = id.String()
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
func (ids *Set[T]) Peek() (T, bool) {
	for id := range *ids {
		return id, true
	}
	return *new(T), false //nolint:gocritic
}
