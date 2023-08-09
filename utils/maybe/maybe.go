// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package maybe

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

// Some returns a new Maybe[T] with the value val.
func Some[T any](val T) Maybe[T] {
	return Maybe[T]{
		value:    val,
		hasValue: true,
	}
}

// Nothing returns a new Maybe[T] with no value.
func Nothing[T any]() Maybe[T] {
	return Maybe[T]{}
}

// IsNothing returns false iff [m] has a value.
func (m Maybe[T]) IsNothing() bool {
	return !m.hasValue
}

// HasValue returns true iff [m] has a value.
func (m Maybe[T]) HasValue() bool {
	return m.hasValue
}

// Value returns the value of [m].
func (m Maybe[T]) Value() T {
	return m.value
}

// Bind returns Nothing iff [m] is Nothing.
// Otherwise applies [f] to the value of [m] and returns the result as a Some.
func Bind[T, U any](m Maybe[T], f func(T) U) Maybe[U] {
	if m.IsNothing() {
		return Nothing[U]()
	}
	return Some(f(m.Value()))
}

// Equal returns true if both m1 and m2 are nothing or have the same value according to [equalFunc].
func Equal[T any](m1 Maybe[T], m2 Maybe[T], equalFunc func(T, T) bool) bool {
	if m1.IsNothing() {
		return m2.IsNothing()
	}

	if m2.IsNothing() {
		return false
	}
	return equalFunc(m1.Value(), m2.Value())
}
