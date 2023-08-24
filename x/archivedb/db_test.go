package archivedb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func getBasicDB() (*archiveDB, error) {
	return newDatabase(
		context.Background(),
		memdb.New(),
	)
}

func TestDbEntries(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatch(10)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(100)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(1000)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@1000")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@1000")))
	require.NoError(t, writer.Write())

	value, height, err := db.Get([]byte("key1"), 10000)
	require.NoError(t, err)
	require.Equal(t, uint64(1000), height)
	require.Equal(t, []byte("value1@1000"), value)

	value, height, err = db.Get([]byte("key1"), 999)
	require.NoError(t, err)
	require.Equal(t, uint64(100), height)
	require.Equal(t, []byte("value1@100"), value)

	value, height, err = db.Get([]byte("key2"), 1999)
	require.NoError(t, err)
	require.Equal(t, uint64(1000), height)
	require.Equal(t, []byte("value2@1000"), value)

	value, height, err = db.Get([]byte("key2"), 999)
	require.NoError(t, err)
	require.Equal(t, uint64(10), height)
	require.Equal(t, []byte("value2@10"), value)

	_, _, err = db.Get([]byte("key1"), 9)
	require.ErrorIs(t, err, database.ErrNotFound)

	_, _, err = db.Get([]byte("key3"), 1999)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestDelete(t *testing.T) {
	db, err := getBasicDB()
	require.NoError(t, err)

	writer := db.NewBatch(10)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@10")))
	require.NoError(t, writer.Put([]byte("key2"), []byte("value2@10")))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(100)
	require.NoError(t, writer.Put([]byte("key1"), []byte("value1@100")))
	require.NoError(t, writer.Write())

	writer = db.NewBatch(1000)
	require.NoError(t, writer.Delete([]byte("key1")))
	require.NoError(t, writer.Delete([]byte("key2")))
	require.NoError(t, writer.Write())

	value, height, err := db.Get([]byte("key1"), 999)
	require.NoError(t, err)
	require.Equal(t, uint64(100), height)
	require.Equal(t, []byte("value1@100"), value)

	value, height, err = db.Get([]byte("key2"), 999)
	require.NoError(t, err)
	require.Equal(t, uint64(10), height)
	require.Equal(t, []byte("value2@10"), value)

	_, _, err = db.Get([]byte("key2"), 1000)
	require.ErrorIs(t, err, database.ErrNotFound)

	_, _, err = db.Get([]byte("key1"), 10000)
	require.ErrorIs(t, err, database.ErrNotFound)
}
