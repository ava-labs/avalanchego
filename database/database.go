// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// For ease of implementation, our database's interface matches Ethereum's
// database implementation. This was to allow use to use Geth code as is for the
// EVM chain.

package database

import (
	"io"
)

// KeyValueReader wraps the Has and Get method of a backing data store.
type KeyValueReader interface {
	// Has retrieves if a key is present in the key-value data store.
	Has(key []byte) (bool, error)

	// Get retrieves the given key if it's present in the key-value data store.
	Get(key []byte) ([]byte, error)
}

// KeyValueWriter wraps the Put method of a backing data store.
type KeyValueWriter interface {
	// Put inserts the given value into the key-value data store.
	Put(key []byte, value []byte) error
}

// KeyValueDeleter wraps the Delete method of a backing data store.
type KeyValueDeleter interface {
	// Delete removes the key from the key-value data store.
	Delete(key []byte) error
}

// KeyValueReaderWriter allows read/write acccess to a backing data store.
type KeyValueReaderWriter interface {
	KeyValueReader
	KeyValueWriter
}

// KeyValueWriterDeleter allows write/delete acccess to a backing data store.
type KeyValueWriterDeleter interface {
	KeyValueWriter
	KeyValueDeleter
}

// KeyValueReaderWriterDeleter allows read/write/delete access to a backing data store.
type KeyValueReaderWriterDeleter interface {
	KeyValueReader
	KeyValueWriter
	KeyValueDeleter
}

// Stater wraps the Stat method of a backing data store.
type Stater interface {
	// Stat returns a particular internal stat of the database.
	Stat(property string) (string, error)
}

// Compacter wraps the Compact method of a backing data store.
type Compacter interface {
	// Compact the underlying DB for the given key range.
	// Specifically, deleted and overwritten versions are discarded,
	// and the data is rearranged to reduce the cost of operations
	// needed to access the data. This operation should typically only
	// be invoked by users who understand the underlying implementation.
	//
	// A nil start is treated as a key before all keys in the DB.
	// And a nil limit is treated as a key after all keys in the DB.
	// Therefore if both are nil then it will compact entire DB.
	Compact(start []byte, limit []byte) error
}

// Database contains all the methods required to allow handling different
// key-value data stores backing the database.
type Database interface {
	KeyValueReaderWriterDeleter
	Batcher
	Iteratee
	Stater
	Compacter
	io.Closer
}
