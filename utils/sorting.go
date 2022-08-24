// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"bytes"
	"sort"

	"github.com/ava-labs/avalanchego/utils/hashing"
	"golang.org/x/exp/constraints"
)

// TODO can we handle sorting where the Less function relies on a codec?

type Sortable[T any] interface {
	Less(T) bool
}

// Sorts the elements of [s].
func SortSliceSortable[T Sortable[T]](s []T) {
	sort.Slice(s, func(i, j int) bool {
		return s[i].Less(s[j])
	})
}

// Sorts the elements of [s].
func SortSliceOrdered[T constraints.Ordered](s []T) {
	sort.Slice(s, func(i, j int) bool {
		return s[i] < s[j]
	})
}

// Sorts the elements of [s] based on their hashes.
func SortByHash[T ~[]byte](s []T) {
	sort.Slice(s, func(i, j int) bool {
		return bytes.Compare(hashing.ComputeHash256(s[i]), hashing.ComputeHash256(s[j])) == -1
	})
}

// Sorts a 2D byte slice.
// Each byte slice is not sorted internally; the byte slices are sorted relative to one another.
func Sort2DByteSlice[T ~[]byte](arr []T) {
	sort.Slice(arr, func(i, j int) bool {
		return bytes.Compare(arr[i], arr[j]) == -1
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
	for i := 0; i < len(s)-1; i++ {
		if bytes.Compare(hashing.ComputeHash256(s[i]), hashing.ComputeHash256(s[i+1])) != -1 {
			return false
		}
	}
	return true
}

// IsSorted2DByteSlice returns true iff [s] is sorted.
// Note that each byte slice need not be sorted internally.
// The byte slices must be sorted relative to one another.
func IsSorted2DByteSlice[T ~[]byte](s []T) bool {
	return sort.SliceIsSorted(s, func(i, j int) bool {
		return bytes.Compare(s[i], s[j]) == -1
	})
}

// IsSortedAndUnique returns true if the elements in the data are unique and sorted.
func IsSortedAndUnique(data sort.Interface) bool {
	for i := 0; i < data.Len()-1; i++ {
		if !data.Less(i, i+1) {
			return false
		}
	}
	return true
}
