// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// Returns a new instance of a T.
func Zero[T any]() T {
	return *new(T)
}

// ZeroSlice sets all values of the provided slice to the type's zero value.
//
// This can be useful to ensure that the garbage collector doesn't hold
// references to values that are no longer desired.
func ZeroSlice[T any](s []T) {
	for i := range s {
		s[i] = *new(T)
	}
}
