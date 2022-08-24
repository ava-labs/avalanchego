// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"bytes"
	"sort"

	"golang.org/x/exp/constraints"
)

type Sortable[T any] interface {
	Less(T) bool
}

// TODO add tests
func SortSlice[T Sortable[T]](s []T) {
	sort.Slice(s, func(i, j int) bool {
		return s[i].Less(s[j])
	})
}

// TODO add tests
func IsSortedAndUniqueSlice[T Sortable[T]](s []T) bool {
	return sort.SliceIsSorted(s, func(i, j int) bool {
		return s[i].Less(s[j])
	})
}

// Sorts a slice of elements that satisfy constraints.Ordered.
// TODO add tests
func SortOrdered[T constraints.Ordered](s []T) {
	sort.Slice(s, func(i, j int) bool {
		return s[i] < s[j]
	})
}

// IsSortedAndUnique returns true if the elements in the data are unique and sorted.
func IsSortedAndUnique(data sort.Interface) bool {
	for i := data.Len() - 2; i >= 0; i-- {
		if !data.Less(i, i+1) {
			return false
		}
	}
	return true
}

// Returns true iff the elements in [s] are unique and sorted.
func IsSortedAndUniqueOrdered[T constraints.Ordered](s []T) bool {
	return sort.SliceIsSorted(s, func(i, j int) bool {
		return s[i] < s[j]
	})
}

// Sort2DBytes sorts a 2D byte slice.
// Each byte slice is not sorted internally; the byte slices are sorted relative to another.
func Sort2DBytes(arr [][]byte) {
	sort.Slice(
		arr,
		func(i, j int) bool {
			return bytes.Compare(arr[i], arr[j]) == -1
		})
}

// IsSorted2DBytes returns true iff [arr] is sorted
func IsSorted2DBytes(arr [][]byte) bool {
	return sort.SliceIsSorted(
		arr,
		func(i, j int) bool {
			return bytes.Compare(arr[i], arr[j]) == -1
		})
}
