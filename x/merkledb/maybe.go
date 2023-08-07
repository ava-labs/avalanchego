// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"

	"golang.org/x/exp/slices"
)

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

// Returns false iff [m] has a value.
func (m Maybe[T]) IsNothing() bool {
	return !m.hasValue
}

// Returns true iff [m] has a value.
func (m Maybe[T]) HasValue() bool {
	return m.hasValue
}

// Returns the value of [m].
func (m Maybe[T]) Value() T {
	return m.value
}

func Clone(m Maybe[[]byte]) Maybe[[]byte] {
	if !m.hasValue {
		return Nothing[[]byte]()
	}
	return Some(slices.Clone(m.value))
}

// MaybeBytesEquals returns true iff [a] and [b] are equal.
func MaybeBytesEquals(a, b Maybe[[]byte]) bool {
	aNothing := a.IsNothing()
	bNothing := b.IsNothing()

	if aNothing {
		return bNothing
	}

	if bNothing {
		return false
	}

	return bytes.Equal(a.Value(), b.Value())
}
