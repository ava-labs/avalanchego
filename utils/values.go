// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// Returns a new instance of a T.
func Zero[T any]() (_ T) {
	return
}

// PtrEqual returns true if x and y are both nil or both point to equal values.
func PtrEqual[T comparable](x, y *T) bool {
	if x == nil || y == nil {
		return x == y
	}
	return *x == *y
}
