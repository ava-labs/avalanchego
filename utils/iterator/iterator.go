package iterator

import "github.com/ava-labs/avalanchego/ids"

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

// Identifiable defines an interface for elements that have unique identifiers.
type Identifiable interface {
	// ID must return a unique identifier for the element in the set.
	ID() ids.ID
}
