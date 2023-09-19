// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func getBasicDB() (*archiveDB, error) {
	return NewArchiveDB(memdb.New())
}

func TestDbEntries(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatch(1)
	require.Equal(t, writer.Height(), uint64(1))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(2)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.Equal(t, writer.Height(), uint64(2))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(3)
	require.Equal(t, writer.Height(), uint64(3))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(4)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.Equal(t, writer.Height(), uint64(4))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(5)
	require.Equal(t, writer.Height(), uint64(5))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(6)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@1000")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@1000")))
	require.Equal(t, writer.Height(), uint64(6))
	require.NoError(t, writer.Write())

	value, height, err := db.Get([]byte("key1"), 2)
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)
	require.Equal(t, []byte("value1@10"), value)

	value, height, err = db.Get([]byte("key1"), 4)
	require.NoError(t, err)
	require.Equal(t, uint64(4), height)
	require.Equal(t, []byte("value1@100"), value)

	value, height, err = db.Get([]byte("key2"), 6)
	require.NoError(t, err)
	require.Equal(t, uint64(6), height)
	require.Equal(t, []byte("value2@1000"), value)

	value, height, err = db.Get([]byte("key2"), 4)
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)
	require.Equal(t, []byte("value2@10"), value)

	_, _, err = db.Get([]byte("key1"), 1)
	require.ErrorIs(t, err, database.ErrNotFound)

	_, _, err = db.Get([]byte("key3"), 6)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestDelete(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatch(1)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.Equal(t, writer.Height(), uint64(1))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(2)
	require.Equal(t, writer.Height(), uint64(2))
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(3)
	require.Equal(t, writer.Height(), uint64(3))
	require.NoError(t, writer.Delete([]byte("key1")))
	require.NoError(t, writer.Delete([]byte("key2")))
	require.NoError(t, writer.Write())

	value, height, err := db.Get([]byte("key1"), 2)
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)
	require.Equal(t, []byte("value1@100"), value)

	value, height, err = db.Get([]byte("key2"), 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), height)
	require.Equal(t, []byte("value2@10"), value)

	_, _, err = db.Get([]byte("key2"), 3)
	require.ErrorIs(t, err, database.ErrNotFound)

	_, _, err = db.Get([]byte("key1"), 3)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestDBKeySpace(t *testing.T) {
	require := require.New(t)

	var (
		key1   = []byte("key1")
		key2   = newDBKey([]byte("key1"), 2)
		key3   = []byte("key3")
		value1 = []byte("value1@1")
		value2 = []byte("value2@2")
		value3 = []byte("value3@3")
	)
	require.NotEqual(key1, key2)
	require.NotEqual(key1, key3)
	require.NotEqual(key2, key3)

	db, err := getBasicDB()
	require.NoError(err)

	writer := db.NewBatch(1)
	require.NoError(err)
	require.NoError(writer.Put(key1, value1))
	require.Equal(uint64(1), writer.Height())
	require.NoError(writer.Write())

	writer = db.NewBatch(2)
	require.NoError(writer.Put(key2, value2))
	require.Equal(uint64(2), writer.Height())
	require.NoError(writer.Write())

	writer = db.NewBatch(3)
	require.NoError(writer.Put(key3, value3))
	require.Equal(uint64(3), writer.Height())
	require.NoError(writer.Write())

	storedHeight, err := database.GetUInt64(db.rawDB, keyHeight)
	require.NoError(err)
	require.Equal(uint64(3), storedHeight)

	val, height, err := db.Get(key1, 3)
	require.NoError(err)
	require.Equal(uint64(1), height)
	require.Equal(value1, val)
}

func TestInvalidBatchHeight(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatch(0)
	err = writer.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)

	writer = db.NewBatch(1)
	require.NoError(t, writer.Write())
	require.NoError(t, writer.Write())

	currentWriter := db.NewBatch(1)
	require.NoError(t, currentWriter.Write())
	require.NoError(t, writer.Write())

	newWriter := db.NewBatch(2)
	require.NoError(t, newWriter.Write())
	// both current_writer and writer are no longer valid because new_writter
	// moved the databas height to 2
	err = currentWriter.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)
	err = writer.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)

	writer = db.NewBatch(50)
	err = writer.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)
}
