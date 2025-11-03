// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// implement a specific interface for firewood
// this is used for some of the firewood performance tests

// Validate that Firewood implements the KVBackend interface
var _ kVBackend = (*Database)(nil)

type kVBackend interface {
	// Returns the current root hash of the trie.
	// Empty trie must return common.Hash{}.
	// Length of the returned slice must be common.HashLength.
	Root() ([]byte, error)

	// Get retrieves the value for the given key.
	// If the key does not exist, it must return (nil, nil).
	Get(key []byte) ([]byte, error)

	// Prefetch loads the intermediary nodes of the given key into memory.
	// The first return value is ignored.
	Prefetch(key []byte) ([]byte, error)

	// After this call, Root() should return the same hash as returned by this call.
	// Note when length of a particular value is zero, it means the corresponding
	// key should be deleted.
	// There may be duplicate keys in the batch provided, and the last one should
	// take effect.
	// Note after this call, the next call to Update must build on the returned root,
	// regardless of whether Commit is called.
	// Length of the returned root must be common.HashLength.
	Update(keys, vals [][]byte) ([]byte, error)

	// After this call, changes related to [root] should be persisted to disk.
	// This may be implemented as no-op if Update already persists changes, or
	// commits happen on a rolling basis.
	// Length of the root slice is guaranteed to be common.HashLength.
	Commit(root []byte) error
}

// Prefetch is a no-op since we don't need to prefetch for Firewood.
func (db *Database) Prefetch(_ []byte) ([]byte, error) {
	if db.handle == nil {
		return nil, errDBClosed
	}

	return nil, nil
}

// Commit is a no-op, since [Database.Update] already persists changes.
func (db *Database) Commit(_ []byte) error {
	if db.handle == nil {
		return errDBClosed
	}

	return nil
}
