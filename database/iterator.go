// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// For ease of implementation, our database's interface matches Ethereum's
// database implementation. This was to allow us to use Geth code as is for the
// EVM chain.

package database

var _ Iterator = (*IteratorError)(nil)

// Iterator iterates over a database's key/value pairs.
//
// When it encounters an error any seek will return false and will yield no key/
// value pairs. The error can be queried by calling the Error method. Calling
// Release is still necessary.
//
// An iterator must be released after use, but it is not necessary to read an
// iterator until exhaustion. An iterator is not safe for concurrent use, but it
// is safe to use multiple iterators concurrently.
type Iterator interface {
	// Next moves the iterator to the next key/value pair. It returns whether
	// the iterator successfully moved to a new key/value pair.
	// The iterator may return false if the underlying database has been closed
	// before the iteration has completed, in which case future calls to Error()
	// must return [ErrClosed].
	Next() bool

	// Error returns any accumulated error. Exhausting all the key/value pairs
	// is not considered to be an error.
	// Error should be called after all key/value pairs have been exhausted i.e.
	// after Next() has returned false.
	Error() error

	// Key returns the key of the current key/value pair, or nil if done.
	// If the database is closed, must still report the current contents.
	// Behavior is undefined after Release is called.
	Key() []byte

	// Value returns the value of the current key/value pair, or nil if done.
	// If the database is closed, must still report the current contents.
	// Behavior is undefined after Release is called.
	Value() []byte

	// Release releases associated resources. Release should always succeed and
	// can be called multiple times without causing an error.
	Release()
}

// Iteratee wraps the NewIterator methods of a backing data store.
type Iteratee interface {
	// NewIterator creates an iterator over the entire keyspace contained within
	// the key-value database.
	NewIterator() Iterator

	// NewIteratorWithStart creates an iterator over a subset of database
	// content starting at a particular initial key.
	NewIteratorWithStart(start []byte) Iterator

	// NewIteratorWithPrefix creates an iterator over a subset of database
	// content with a particular key prefix.
	NewIteratorWithPrefix(prefix []byte) Iterator

	// NewIteratorWithStartAndPrefix creates an iterator over a subset of
	// database content with a particular key prefix starting at a specified
	// key.
	NewIteratorWithStartAndPrefix(start, prefix []byte) Iterator
}

// IteratorError does nothing and returns the provided error
type IteratorError struct {
	Err error
}

func (*IteratorError) Next() bool {
	return false
}

func (i *IteratorError) Error() error {
	return i.Err
}

func (*IteratorError) Key() []byte {
	return nil
}

func (*IteratorError) Value() []byte {
	return nil
}

func (*IteratorError) Release() {}
