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

func getBasicDB() (*archiveDB, error) {
	return NewArchiveDB(
		context.Background(),
		memdb.New(),
	)
}

func TestDbEntries(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatch()
	require.Equal(t, writer.Height(), uint64(1))
	require.NoError(t, writer.Write())

	writer = db.NewBatch()
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.Equal(t, writer.Height(), uint64(2))
	require.NoError(t, writer.Write())

	writer = db.NewBatch()
	require.Equal(t, writer.Height(), uint64(3))
	require.NoError(t, writer.Write())

	writer = db.NewBatch()
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.Equal(t, writer.Height(), uint64(4))
	require.NoError(t, writer.Write())

	writer = db.NewBatch()
	require.Equal(t, writer.Height(), uint64(5))
	require.NoError(t, writer.Write())

	writer = db.NewBatch()
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

	writer := db.NewBatch()
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.Equal(t, writer.Height(), uint64(1))
	require.NoError(t, writer.Write())

	writer = db.NewBatch()
	require.Equal(t, writer.Height(), uint64(2))
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.NoError(t, writer.Write())

	writer = db.NewBatch()
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
