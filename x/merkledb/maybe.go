// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

// Maybe T = Some T | Nothing.
// A data wrapper that allows values to be something [Some T] or nothing [Nothing].
// Maybe is used to wrap types:
// * That can't be represented by nil.
// * That use nil as a valid value instead of an indicator of a missing value.
// For more info see https://en.wikipedia.org/wiki/Option_type
type Maybe[T any] struct {
	hasValue bool
	value    T
}

// Returns a new Maybe[T] with the value val.
func Some[T any](val T) Maybe[T] {
	return Maybe[T]{
		value:    val,
		hasValue: true,
	}
}

// Returns a new Maybe[T] with no value.
func Nothing[T any]() Maybe[T] {
	return Maybe[T]{}
}

// Returns true iff [m] has a value.
func (m Maybe[T]) IsNothing() bool {
	return !m.hasValue
}

// Returns the value of [m].
func (m Maybe[T]) Value() T {
	return m.value
}
