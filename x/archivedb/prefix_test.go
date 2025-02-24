// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

type limitIterationDB struct {
	database.Database
}

func (db *limitIterationDB) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *limitIterationDB) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *limitIterationDB) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (db *limitIterationDB) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return &limitIterationIterator{
		Iterator: db.Database.NewIteratorWithStartAndPrefix(start, prefix),
	}
}

type limitIterationIterator struct {
	database.Iterator
	exhausted bool
}

func (it *limitIterationIterator) Next() bool {
	if it.exhausted {
		return false
	}
	it.exhausted = true
	return it.Iterator.Next()
}

func TestDBEfficientLookups(t *testing.T) {
	require := require.New(t)

	var (
		key             = []byte("key")
		maliciousKey, _ = newDBKeyFromUser(key, 2)
	)

	db := New(&limitIterationDB{Database: memdb.New()})

	batch := db.NewBatch(1)
	require.NoError(batch.Put(key, []byte("value")))
	require.NoError(batch.Write())

	for i := 0; i < 10000; i++ {
		batch = db.NewBatch(uint64(i) + 2)
		require.NoError(batch.Put(maliciousKey, []byte{byte(i)}))
		require.NoError(batch.Write())
	}

	reader := db.Open(10001)
	value, err := reader.Get(key)
	require.NoError(err)
	require.Equal([]byte("value"), value)

	value, height, found, err := reader.GetEntry(key)
	require.NoError(err)
	require.True(found)
	require.Equal(uint64(1), height)
	require.Equal([]byte("value"), value)
}

func TestDBMoreEfficientLookups(t *testing.T) {
	require := require.New(t)

	var (
		key          = []byte("key")
		maliciousKey = []byte("key\xff\xff\xff\xff\xff\xff\xff\xfd")
	)

	db := New(&limitIterationDB{Database: memdb.New()})

	batch := db.NewBatch(1)
	require.NoError(batch.Put(key, []byte("value")))
	require.NoError(batch.Write())

	for i := 2; i < 10000; i++ {
		batch = db.NewBatch(uint64(i))
		require.NoError(batch.Put(maliciousKey, []byte{byte(i)}))
		require.NoError(batch.Write())
	}

	reader := db.Open(10001)
	value, err := reader.Get(key)
	require.NoError(err)
	require.Equal([]byte("value"), value)

	value, height, found, err := reader.GetEntry(key)
	require.NoError(err)
	require.True(found)
	require.Equal(uint64(1), height)
	require.Equal([]byte("value"), value)
}
