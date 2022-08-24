// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"strings"

	"github.com/ava-labs/avalanchego/utils"
	"golang.org/x/exp/constraints"
)

// Settable describes an element that can be in a set.
type Settable interface {
	constraints.Ordered
	comparable
	String() string
}

// Set2 is a set of IDs
type Set2[T Settable] map[T]struct{}

// Return a new set with initial capacity [size].
// More or less than [size] elements can be added to this set.
// Using NewSet2() rather than ids.Set2{} is just an optimization that can
// be used if you know how many elements will be put in this set.
func NewSet2[T Settable](size int) Set2[T] {
	if size < 0 {
		return Set2[T]{}
	}
	return make(map[T]struct{}, size)
}

func (ids *Set2[T]) init(size int) {
	if *ids == nil {
		if minSetSize > size {
			size = minSetSize
		}
		*ids = make(map[T]struct{}, size)
	}
}

// Add all the ids to this set, if the id is already in the set, nothing happens
func (ids *Set2[T]) Add(elts ...T) {
	ids.init(2 * len(elts))
	for _, id := range elts {
		(*ids)[id] = struct{}{}
	}
}

// Union adds all the ids from the provided set to this set.
func (ids *Set2[T]) Union(set Set2[T]) {
	ids.init(2 * set.Len())
	for id := range set {
		(*ids)[id] = struct{}{}
	}
}

// Difference removes all the ids from the provided set to this set.
func (ids *Set2[T]) Difference(set Set2[T]) {
	for elt := range set {
		delete(*ids, elt)
	}
}

// Contains returns true if the set contains this id, false otherwise
func (ids *Set2[T]) Contains(elt T) bool {
	_, contains := (*ids)[elt]
	return contains
}

// Overlaps returns true if the intersection of the set is non-empty
func (ids *Set2[T]) Overlaps(big Set2[T]) bool {
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
func (ids Set2[T]) Len() int { return len(ids) }

// Remove all the id from this set, if the id isn't in the set, nothing happens
func (ids *Set2[T]) Remove(elts ...T) {
	for _, elt := range elts {
		delete(*ids, elt)
	}
}

// Clear empties this set
func (ids *Set2[T]) Clear() {
	if len(*ids) > clearSizeThreshold {
		*ids = nil
		return
	}
	for elt := range *ids {
		delete(*ids, elt)
	}
}

// List converts this set into a list
func (ids Set2[T]) List() []T {
	idList := make([]T, ids.Len())
	i := 0
	for id := range ids {
		idList[i] = id
		i++
	}
	return idList
}

// SortedList returns this set as a sorted list
func (ids Set2[T]) SortedList() []T {
	lst := ids.List()
	utils.SortSliceOrdered(lst)
	return lst
}

// CappedList returns a list of length at most [size].
// Size should be >= 0. If size < 0, returns nil.
func (ids Set2[T]) CappedList(size int) []T {
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
func (ids Set2[T]) Equals(other Set2[T]) bool {
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
func (ids Set2[T]) String() string {
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
func (ids *Set2[T]) Pop() (T, bool) {
	for id := range *ids {
		delete(*ids, id)
		return id, true
	}
	var t T
	return t, false
}

func (ids *Set2[T]) MarshalJSON() ([]byte, error) {
	idsList := ids.List()
	utils.SortSliceOrdered(idsList)
	return json.Marshal(idsList)
}
