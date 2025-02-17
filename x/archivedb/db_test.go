// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestDBEntries(t *testing.T) {
	require := require.New(t)

	db := New(memdb.New())

	batch := db.NewBatch(1)
	require.NoError(batch.Write())

	batch = db.NewBatch(2)
	require.NoError(batch.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(batch.Put([]byte("key2"), []byte("value2@10")))
	require.NoError(batch.Write())

	batch = db.NewBatch(3)
	require.NoError(batch.Write())

	batch = db.NewBatch(4)
	require.NoError(batch.Put([]byte("key1"), []byte("value1@100")))
	require.NoError(batch.Write())

	batch = db.NewBatch(5)
	require.NoError(batch.Write())

	batch = db.NewBatch(6)
	require.NoError(batch.Put([]byte("key1"), []byte("value1@1000")))
	require.NoError(batch.Put([]byte("key2"), []byte("value2@1000")))
	require.NoError(batch.Write())

	reader := db.Open(2)
	value, err := reader.Get([]byte("key1"))
	require.NoError(err)
	require.Equal([]byte("value1@10"), value)

	value, height, exists, err := reader.GetEntry([]byte("key1"))
	require.NoError(err)
	require.True(exists)
	require.Equal([]byte("value1@10"), value)
	require.Equal(uint64(2), height)

	reader = db.Open(4)
	value, err = reader.Get([]byte("key1"))
	require.NoError(err)
	require.Equal([]byte("value1@100"), value)

	value, height, exists, err = reader.GetEntry([]byte("key1"))
	require.NoError(err)
	require.True(exists)
	require.Equal([]byte("value1@100"), value)
	require.Equal(uint64(4), height)

	reader = db.Open(6)
	value, err = reader.Get([]byte("key2"))
	require.NoError(err)
	require.Equal([]byte("value2@1000"), value)

	value, height, exists, err = reader.GetEntry([]byte("key2"))
	require.NoError(err)
	require.True(exists)
	require.Equal([]byte("value2@1000"), value)
	require.Equal(uint64(6), height)

	reader = db.Open(4)
	value, err = reader.Get([]byte("key2"))
	require.NoError(err)
	require.Equal([]byte("value2@10"), value)

	value, height, exists, err = reader.GetEntry([]byte("key2"))
	require.NoError(err)
	require.True(exists)
	require.Equal([]byte("value2@10"), value)
	require.Equal(uint64(2), height)

	exists, err = reader.Has([]byte("key2"))
	require.NoError(err)
	require.True(exists)

	reader = db.Open(1)
	_, err = reader.Get([]byte("key1"))
	require.ErrorIs(err, database.ErrNotFound)

	exists, err = reader.Has([]byte("key1"))
	require.NoError(err)
	require.False(exists)

	reader = db.Open(1)
	_, err = reader.Get([]byte("key3"))
	require.ErrorIs(err, database.ErrNotFound)
}

func TestDelete(t *testing.T) {
	require := require.New(t)

	db := New(memdb.New())

	batch := db.NewBatch(1)
	require.NoError(batch.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(batch.Put([]byte("key2"), []byte("value2@10")))
	require.NoError(batch.Write())

	batch = db.NewBatch(2)
	require.NoError(batch.Put([]byte("key1"), []byte("value1@100")))
	require.NoError(batch.Write())

	batch = db.NewBatch(3)
	require.NoError(batch.Delete([]byte("key1")))
	require.NoError(batch.Delete([]byte("key2")))
	require.NoError(batch.Write())

	reader := db.Open(2)
	value, err := reader.Get([]byte("key1"))
	require.NoError(err)
	require.Equal([]byte("value1@100"), value)

	value, height, exists, err := reader.GetEntry([]byte("key1"))
	require.NoError(err)
	require.True(exists)
	require.Equal(uint64(2), height)
	require.Equal([]byte("value1@100"), value)

	reader = db.Open(1)
	value, err = reader.Get([]byte("key2"))
	require.NoError(err)
	require.Equal([]byte("value2@10"), value)

	value, height, exists, err = reader.GetEntry([]byte("key2"))
	require.NoError(err)
	require.True(exists)
	require.Equal(uint64(1), height)
	require.Equal([]byte("value2@10"), value)

	reader = db.Open(3)
	_, err = reader.Get([]byte("key2"))
	require.ErrorIs(err, database.ErrNotFound)

	_, err = reader.Get([]byte("key1"))
	require.ErrorIs(err, database.ErrNotFound)

	_, height, exists, err = reader.GetEntry([]byte("key1"))
	require.NoError(err)
	require.False(exists)
	require.Equal(uint64(3), height)

	_, _, _, err = reader.GetEntry([]byte("key4"))
	require.ErrorIs(err, database.ErrNotFound)
}

func TestDBKeySpace(t *testing.T) {
	require := require.New(t)

	var (
		key1    = []byte("key1")
		key2, _ = newDBKeyFromUser([]byte("key1"), 2)
		key3    = []byte("key3")
		value1  = []byte("value1@1")
		value2  = []byte("value2@2")
		value3  = []byte("value3@3")
	)
	require.NotEqual(key1, key2)
	require.NotEqual(key1, key3)
	require.NotEqual(key2, key3)

	db := New(memdb.New())

	batch := db.NewBatch(1)
	require.NoError(batch.Put(key1, value1))
	require.NoError(batch.Write())

	batch = db.NewBatch(2)
	require.NoError(batch.Put(key2, value2))
	require.NoError(batch.Write())

	batch = db.NewBatch(3)
	require.NoError(batch.Put(key3, value3))
	require.NoError(batch.Write())

	storedHeight, err := db.Height()
	require.NoError(err)
	require.Equal(uint64(3), storedHeight)

	reader := db.Open(3)
	value, err := reader.Get(key1)
	require.NoError(err)
	require.Equal(value1, value)

	value, height, exists, err := reader.GetEntry(key1)
	require.NoError(err)
	require.True(exists)
	require.Equal(uint64(1), height)
	require.Equal(value1, value)
}

func TestSkipHeight(t *testing.T) {
	require := require.New(t)

	db := New(memdb.New())

	_, err := db.Height()
	require.ErrorIs(err, database.ErrNotFound)

	batch := db.NewBatch(0)
	require.NoError(batch.Write())

	height, err := db.Height()
	require.NoError(err)
	require.Zero(height)

	batch = db.NewBatch(10)
	require.NoError(batch.Write())

	height, err = db.Height()
	require.NoError(err)
	require.Equal(uint64(10), height)
}
