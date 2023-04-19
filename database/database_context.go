// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// For ease of implementation, our database's interface matches Ethereum's
// database implementation. This was to allow use to use Geth code as is for the
// EVM chain.

package database

import (
	"context"
	"io"

	"github.com/ava-labs/avalanchego/api/health"
)

// KeyValueReader wraps the Has and Get method of a backing data store.
type KeyValueReaderWithContext interface {
	// Has retrieves if a key is present in the key-value data store.
	//
	// Note: [key] is safe to modify and read after calling Has.
	Has(ctx context.Context, key []byte) (bool, error)

	// Get retrieves the given key if it's present in the key-value data store.
	//
	// Note: [key] is safe to modify and read after calling Get.
	// The returned byte slice is safe to read, but cannot be modified.
	Get(ctx context.Context, key []byte) ([]byte, error)
}

// KeyValueWriter wraps the Put method of a backing data store.
type KeyValueWriterWithContext interface {
	// Put inserts the given value into the key-value data store.
	//
	// Note: [key] and [value] are safe to modify and read after calling Put.
	//
	// If [value] is nil or an empty slice, then when it's retrieved
	// it may be nil or an empty slice.
	//
	// Similarly, a nil [key] is treated the same as an empty slice.
	Put(ctx context.Context, key []byte, value []byte) error
}

// KeyValueDeleter wraps the Delete method of a backing data store.
type KeyValueDeleterWithContext interface {
	// Delete removes the key from the key-value data store.
	//
	// Note: [key] is safe to modify and read after calling Delete.
	Delete(ctx context.Context, key []byte) error
}

// KeyValueReaderWriter allows read/write acccess to a backing data store.
type KeyValueReaderWriterWithContext interface {
	KeyValueReaderWithContext
	KeyValueWriterWithContext
}

// KeyValueWriterDeleter allows write/delete acccess to a backing data store.
type KeyValueWriterDeleterWithContext interface {
	KeyValueWriterWithContext
	KeyValueDeleterWithContext
}

// KeyValueReaderWriterDeleter allows read/write/delete access to a backing data store.
type KeyValueReaderWriterDeleterWithContext interface {
	KeyValueReaderWithContext
	KeyValueWriterWithContext
	KeyValueDeleterWithContext
}

// Compacter wraps the Compact method of a backing data store.
type CompacterWithContext interface {
	// Compact the underlying DB for the given key range.
	// Specifically, deleted and overwritten versions are discarded,
	// and the data is rearranged to reduce the cost of operations
	// needed to access the data. This operation should typically only
	// be invoked by users who understand the underlying implementation.
	//
	// A nil start is treated as a key before all keys in the DB.
	// And a nil limit is treated as a key after all keys in the DB.
	// Therefore if both are nil then it will compact entire DB.
	//
	// Note: [start] and [limit] are safe to modify and read after calling Compact.
	Compact(ctx context.Context, start []byte, limit []byte) error
}

// Database contains all the methods required to allow handling different
// key-value data stores backing the database.
type DatabaseWithContext interface {
	KeyValueReaderWriterDeleterWithContext
	BatcherWithContext
	Iteratee
	CompacterWithContext
	io.Closer
	health.Checker
}
