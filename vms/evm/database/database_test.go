// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/dbtest"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
)

// testDatabase wraps the production database with test-only snapshot functionality
type testDatabase struct {
	ethdb.KeyValueStore
}

// Creates a snapshot by iterating over the entire database and copying key-value pairs.
func (db testDatabase) NewSnapshot() (ethdb.Snapshot, error) {
	snapshotData := make(map[string][]byte)

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
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

// testSnapshot implements ethdb.Snapshot by storing a copy of the database state.
type testSnapshot struct {
	data map[string][]byte
}

func (t *testSnapshot) Get(key []byte) ([]byte, error) {
	value, ok := t.data[string(key)]
	if !ok {
		return nil, errors.New("not found")
	}
	return value, nil
}

func (t *testSnapshot) Has(key []byte) (bool, error) {
	_, exists := t.data[string(key)]
	return exists, nil
}

func (*testSnapshot) Release() {
	// No cleanup needed for snapshot
}

func (t *testSnapshot) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// Create a slice of key-value pairs that match the prefix and start criteria
	pairs := make([]kvPair, 0, len(t.data))

	for keyStr, value := range t.data {
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

	// Sort by key for consistent iteration
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

func TestInterface(t *testing.T) {
	dbtest.TestDatabaseSuite(t, func() ethdb.KeyValueStore {
		return &testDatabase{KeyValueStore: New(memdb.New())}
	})
}

func TestProductionErrors(t *testing.T) {
	t.Run("NewSnapshot_ReturnsError", func(t *testing.T) {
		wrappedDB := New(memdb.New())
		_, err := wrappedDB.NewSnapshot()
		require.ErrorIs(t, err, errSnapshotNotSupported)
	})

	t.Run("Stat_ReturnsError", func(t *testing.T) {
		wrappedDB := New(memdb.New())
		_, err := wrappedDB.Stat("test")
		require.ErrorIs(t, err, errStatNotSupported)
	})
}
