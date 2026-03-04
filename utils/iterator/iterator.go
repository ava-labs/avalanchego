// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

// Iterator defines an interface for iterating over a set.
type Iterator[T any] interface {
	// Next attempts to move the iterator to the next element in the set. It
	// returns false once there are no more elements to return.
	Next() bool

	// Value returns the value of the current element. Value should only be called
	// after a call to Next which returned true.
	Value() T

	// Release any resources associated with the iterator. This must be called
	// after the iterator is no longer needed.
	Release()
}
