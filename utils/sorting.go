// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"bytes"
	"sort"

	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

// TODO can we handle sorting where the Less function relies on a codec?

type Sortable[T any] interface {
	Less(T) bool
}

// Sorts the elements of [s].
func Sort[T Sortable[T]](s []T) {
	slices.SortFunc(s, func(i, j T) bool {
		return i.Less(j)
	})
}

// Sorts the elements of [s] based on their hashes.
func SortByHash[T ~[]byte](s []T) {
	slices.SortFunc(s, func(i, j T) bool {
		iHash := hashing.ComputeHash256(i)
		jHash := hashing.ComputeHash256(j)
		return bytes.Compare(iHash, jHash) == -1
	})
}

// Sorts a 2D byte slice.
// Each byte slice is not sorted internally; the byte slices are sorted relative
// to one another.
func SortBytes[T ~[]byte](arr []T) {
	slices.SortFunc(arr, func(i, j T) bool {
		return bytes.Compare(i, j) == -1
	})
}

// Returns true iff the elements in [s] are unique and sorted.
func IsSortedAndUniqueSortable[T Sortable[T]](s []T) bool {
	for i := 0; i < len(s)-1; i++ {
		if !s[i].Less(s[i+1]) {
			return false
		}
	}
	return true
}

// Returns true iff the elements in [s] are unique and sorted.
func IsSortedAndUniqueOrdered[T constraints.Ordered](s []T) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] >= s[i+1] {
			return false
		}
	}
	return true
}

// Returns true iff the elements in [s] are unique and sorted
// based by their hashes.
func IsSortedAndUniqueByHash[T ~[]byte](s []T) bool {
	if len(s) <= 1 {
		return true
	}
	rightHash := hashing.ComputeHash256(s[0])
	for i := 1; i < len(s); i++ {
		leftHash := rightHash
		rightHash = hashing.ComputeHash256(s[i])
		if bytes.Compare(leftHash, rightHash) != -1 {
			return false
		}
	}
	return true
}

// Returns true iff the elements in [s] are unique.
func IsUnique[T comparable](elts []T) bool {
	// Can't use set.Set because it'd be a circular import.
	asMap := make(map[T]struct{}, len(elts))
	for _, elt := range elts {
		if _, ok := asMap[elt]; ok {
			return false
		}
		asMap[elt] = struct{}{}
	}
	return true
}

// IsSortedAndUnique returns true if the elements in the data are unique and
// sorted.
//
// Deprecated: Use one of the other [IsSortedAndUnique...] functions instead.
func IsSortedAndUnique(data sort.Interface) bool {
	for i := 0; i < data.Len()-1; i++ {
		if !data.Less(i, i+1) {
			return false
		}
	}
	return true
}
