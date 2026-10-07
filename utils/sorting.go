// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

// TODO can we handle sorting where the Compare function relies on a codec?

type Sortable[T any] interface {
	Compare(T) int
}

// Sort sorts the elements of s.
func Sort[T Sortable[T]](s []T) {
	slices.SortFunc(s, T.Compare)
}

// SortByHash sorts the elements of s based on their hashes.
func SortByHash[T ~[]byte](s []T) {
	slices.SortFunc(s, func(i, j T) int {
		iHash := hashing.ComputeHash256(i)
		jHash := hashing.ComputeHash256(j)
		return bytes.Compare(iHash, jHash)
	})
}

// IsSortedBytes returns true iff the elements in s are sorted.
func IsSortedBytes[T ~[]byte](s []T) bool {
	for i := 0; i < len(s)-1; i++ {
		if bytes.Compare(s[i], s[i+1]) == 1 {
			return false
		}
	}
	return true
}

// IsSortedAndUnique returns true iff the elements in s are unique and sorted.
func IsSortedAndUnique[T Sortable[T]](s []T) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i].Compare(s[i+1]) >= 0 {
			return false
		}
	}
	return true
}

// IsSortedAndUniqueOrdered returns true iff the elements in s are unique and
// sorted.
func IsSortedAndUniqueOrdered[T cmp.Ordered](s []T) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] >= s[i+1] {
			return false
		}
	}
	return true
}

// IsSortedAndUniqueByHash returns true iff the elements in s are unique and
// sorted based on their hashes.
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
