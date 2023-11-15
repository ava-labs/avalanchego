// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"bytes"

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
	slices.SortFunc(s, T.Less)
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
func SortBytes[T ~[]byte](s []T) {
	slices.SortFunc(s, func(i, j T) bool {
		return bytes.Compare(i, j) == -1
	})
}

// Returns true iff the elements in [s] are sorted.
func IsSortedBytes[T ~[]byte](s []T) bool {
	for i := 0; i < len(s)-1; i++ {
		if bytes.Compare(s[i], s[i+1]) == 1 {
			return false
		}
	}
	return true
}

// Returns true iff the elements in [s] are unique and sorted.
func IsSortedAndUnique[T Sortable[T]](s []T) bool {
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
