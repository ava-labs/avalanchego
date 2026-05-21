// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// DeleteIndex moves the last element in the slice to index [i] and shrinks the
// size of the slice by 1.
//
// This is an O(1) operation that allows the removal of an element from a slice
// when the order of the slice is not important.
//
// If [i] is out of bounds, this function will panic.
func DeleteIndex[S ~[]E, E any](s S, i int) S {
	newSize := len(s) - 1
	s[i] = s[newSize]
	s[newSize] = Zero[E]()
	return s[:newSize]
}
