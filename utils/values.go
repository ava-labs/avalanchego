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

// First returns the first of two values, typically a function's first result.
func First[A, B any](a A, _ B) A {
	return a
}

// Second returns the second of two values, typically a function's second result.
func Second[A, B any](_ A, b B) B {
	return b
}
