// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"context"
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
		key          = []byte("key")
		value        = []byte("value")
		maliciousKey = newInternalKey(key, 2).Bytes()
	)

	db, err := NewArchiveDB(
		context.Background(),
		&limitIterationDB{
			Database: memdb.New(),
		},
	)
	require.NoError(err)

	writer, err := db.NewBatch()
	require.NoError(err)
	require.NoError(writer.Put(key, value))
	require.Equal(uint64(1), writer.Height())
	require.NoError(writer.Write())

	for i := 0; i < 10000; i++ {
		writer, err = db.NewBatch()
		require.NoError(err)

		require.NoError(writer.Put(maliciousKey, []byte{byte(i)}))
		require.NoError(writer.Write())
	}

	val, height, err := db.Get(key, db.currentHeight)
	require.NoError(err)
	require.Equal(uint64(1), height)
	require.Equal(value, val)
}
