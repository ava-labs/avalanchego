// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"bytes"
	"slices"

	"github.com/ava-labs/libevm/ethdb"

	avalanchegodb "github.com/ava-labs/avalanchego/database"
)

// testSnapshot implements ethdb.Snapshot by storing a copy of the database state.
// This is a test-only implementation that loads the entire database into memory
// for the purpose of satisfying the dbtest.TestDatabaseSuite interface requirements.
// It is NOT suitable for production use due to memory and performance implications.
type testSnapshot struct {
	data map[string][]byte
}

func (s *testSnapshot) Get(key []byte) ([]byte, error) {
	value, exists := s.data[string(key)]
	if !exists {
		return nil, avalanchegodb.ErrNotFound
	}
	return value, nil
}

func (s *testSnapshot) Has(key []byte) (bool, error) {
	_, exists := s.data[string(key)]
	return exists, nil
}

func (*testSnapshot) Release() {
	// No cleanup needed for snapshot
}

func (s *testSnapshot) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// Create a slice of key-value pairs that match the prefix and start criteria
	pairs := make([]kvPair, 0, len(s.data))

	for keyStr, value := range s.data {
		key := []byte(keyStr)

		if prefix != nil && len(key) < len(prefix) {
			continue
		}
		if prefix != nil && !bytes.HasPrefix(key, prefix) {
			continue
		}

		if start != nil && bytes.Compare(key, start) < 0 {
			continue
		}

		pairs = append(pairs, kvPair{key: key, value: value})
	}

	slices.SortFunc(pairs, func(a, b kvPair) int {
		return bytes.Compare(a.key, b.key)
	})

	return &testSnapshotIterator{pairs: pairs, index: -1}
}

type kvPair struct {
	key   []byte
	value []byte
}

type testSnapshotIterator struct {
	pairs []kvPair
	index int
}

func (it *testSnapshotIterator) Next() bool {
	it.index++
	return it.index < len(it.pairs)
}

func (it *testSnapshotIterator) Key() []byte {
	if it.index < 0 || it.index >= len(it.pairs) {
		return nil
	}
	return it.pairs[it.index].key
}

func (it *testSnapshotIterator) Value() []byte {
	if it.index < 0 || it.index >= len(it.pairs) {
		return nil
	}
	return it.pairs[it.index].value
}

func (*testSnapshotIterator) Release() {
	// No cleanup needed for snapshot iterator
}

func (*testSnapshotIterator) Error() error {
	return nil
}

// testDatabase wraps the production database with test-only snapshot functionality
type testDatabase struct {
	database
}

// NewSnapshot creates a test-only snapshot that captures the current database state.
// This implementation is designed for test suite compliance only and is NOT suitable
// for production use as it loads the entire database into memory.
func (db testDatabase) NewSnapshot() (ethdb.Snapshot, error) {
	// NOTE: This snapshot implementation is designed for test suite compliance only.
	// It is NOT suitable for production use as it:
	// - Iterates over the entire database at snapshot creation time
	// - Stores a complete copy of all data in memory
	// - Does not provide efficient incremental snapshots
	// - May cause memory issues with large databases
	//
	// This implementation exists solely to satisfy the dbtest.TestDatabaseSuite
	// interface requirements, particularly the TestIteratorSnapshot test which
	// verifies that iterators created from snapshots reflect the database state
	// at the time of snapshot creation, not the current state.
	//
	// In production code, snapshots are never actually used, so this workaround
	// allows us to get test coverage without implementing a proper snapshot
	// mechanism that would require significant database engine changes.

	// Create a snapshot by iterating over the entire database and copying key-value pairs
	snapshotData := make(map[string][]byte)

	// Iterate over all keys in the database
	iter := db.db.NewIterator()
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		// Make copies to ensure the snapshot is independent
		keyCopy := make([]byte, len(key))
		valueCopy := make([]byte, len(value))
		copy(keyCopy, key)
		copy(valueCopy, value)
		snapshotData[string(keyCopy)] = valueCopy
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	return &testSnapshot{data: snapshotData}, nil
}
