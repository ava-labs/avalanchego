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

func TestInterfaceX(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)
	database.TestBatchReuse(t, db)
}

func TestInterface(t *testing.T) {
	var tests = []func(t *testing.T, db database.Database){
		database.TestSimpleKeyValue,
		database.TestOverwriteKeyValue,
		database.TestEmptyKey,
		database.TestKeyEmptyValue,
		database.TestSimpleKeyValueClosed,
		database.TestNewBatchClosed,
		database.TestBatchPut,
		database.TestBatchDelete,
		database.TestBatchReset,
		database.TestBatchReuse,
		database.TestBatchRewrite,
		database.TestBatchReplay,
		database.TestBatchReplayPropagateError,
		database.TestBatchInner,
		database.TestBatchLargeSize,
		database.TestCompactNoPanic,
		database.TestMemorySafetyDatabase,
		database.TestMemorySafetyBatch,
		database.TestModifyValueAfterPut,
		database.TestModifyValueAfterBatchPut,
		database.TestModifyValueAfterBatchPutReplay,
		database.TestConcurrentBatches,
		database.TestManySmallConcurrentKVPairBatches,
		database.TestPutGetEmpty,
	}

	for _, test := range tests {
		db, err := getBasicDB()
		require.NoError(t, err)
		test(t, db)
	}
}

func TestDbEntries(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatchAtHeight(1)
	require.Equal(t, writer.Height(), uint64(1))
	require.NoError(t, writer.Write())

	writer = db.NewBatchAtHeight(2)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.Equal(t, writer.Height(), uint64(2))
	require.NoError(t, writer.Write())

	writer = db.NewBatchAtHeight(3)
	require.Equal(t, writer.Height(), uint64(3))
	require.NoError(t, writer.Write())

	writer = db.NewBatchAtHeight(4)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.Equal(t, writer.Height(), uint64(4))
	require.NoError(t, writer.Write())

	writer = db.NewBatchAtHeight(5)
	require.Equal(t, writer.Height(), uint64(5))
	require.NoError(t, writer.Write())

	writer = db.NewBatchAtHeight(6)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@1000")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@1000")))
	require.Equal(t, writer.Height(), uint64(6))
	require.NoError(t, writer.Write())

	reader, err := db.GetHeightReader(2)
	require.NoError(t, err)
	value, err := reader.Get([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, []byte("value1@10"), value)
	height, err := reader.GetHeight([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)

	reader, err = db.GetHeightReader(4)
	require.NoError(t, err)
	value, err = reader.Get([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, []byte("value1@100"), value)
	height, err = reader.GetHeight([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, uint64(4), height)

	reader, err = db.GetHeightReader(6)
	require.NoError(t, err)
	value, err = reader.Get([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, []byte("value2@1000"), value)
	height, err = reader.GetHeight([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, uint64(6), height)

	reader, err = db.GetHeightReader(4)
	require.NoError(t, err)
	value, err = reader.Get([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, []byte("value2@10"), value)
	height, err = reader.GetHeight([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)

	reader, err = db.GetHeightReader(1)
	require.NoError(t, err)

	_, err = reader.Get([]byte("key1"))
	require.ErrorIs(t, err, database.ErrNotFound)
	exists, err := reader.Has([]byte("key1"))
	require.NoError(t, err)
	require.False(t, exists)

	reader, err = db.GetHeightReader(6)
	require.NoError(t, err)

	_, err = reader.Get([]byte("key3"))
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestDelete(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatchAtHeight(1)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.Equal(t, writer.Height(), uint64(1))
	require.NoError(t, writer.Write())

	writer = db.NewBatchAtHeight(2)
	require.Equal(t, writer.Height(), uint64(2))
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.NoError(t, writer.Write())

	writer = db.NewBatchAtHeight(3)
	require.Equal(t, writer.Height(), uint64(3))
	require.NoError(t, writer.Delete([]byte("key1")))
	require.NoError(t, writer.Delete([]byte("key2")))
	require.NoError(t, writer.Write())

	reader, err := db.GetHeightReader(2)
	require.NoError(t, err)
	value, err := reader.Get([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, []byte("value1@100"), value)
	height, err := reader.GetHeight([]byte("key1"))
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)

	reader, err = db.GetHeightReader(1)
	require.NoError(t, err)
	value, err = reader.Get([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, []byte("value2@10"), value)
	height, err = reader.GetHeight([]byte("key2"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), height)

	_, err = db.Get([]byte("key2"))
	require.ErrorIs(t, err, database.ErrNotFound)

	_, err = db.Get([]byte("key1"))
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

	writer := db.NewBatchAtHeight(1)
	require.NoError(err)
	require.NoError(writer.Put(key1, value1))
	require.Equal(uint64(1), writer.Height())
	require.NoError(writer.Write())

	writer = db.NewBatchAtHeight(2)
	require.NoError(writer.Put(key2, value2))
	require.Equal(uint64(2), writer.Height())
	require.NoError(writer.Write())

	writer = db.NewBatchAtHeight(3)
	require.NoError(writer.Put(key3, value3))
	require.Equal(uint64(3), writer.Height())
	require.NoError(writer.Write())

	storedHeight, err := database.GetUInt64(db.inner, keyHeight)
	require.NoError(err)
	require.Equal(uint64(3), storedHeight)

	val, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, val)
	height, err := db.GetHeight(key1)
	require.Equal(uint64(1), height)
}

func TestInvalidBatchHeight(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatchAtHeight(0)
	err = writer.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)

	writer = db.NewBatchAtHeight(1)
	require.NoError(t, writer.Write())
	require.NoError(t, writer.Write())

	currentWriter := db.NewBatchAtHeight(1)
	require.NoError(t, currentWriter.Write())
	require.NoError(t, writer.Write())

	newWriter := db.NewBatchAtHeight(2)
	require.NoError(t, newWriter.Write())
	// both current_writer and writer are no longer valid because new_writter
	// moved the databas height to 2
	err = currentWriter.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)
	err = writer.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)

	writer = db.NewBatchAtHeight(50)
	err = writer.Write()
	require.ErrorIs(t, err, ErrInvalidBatchHeight)
}
