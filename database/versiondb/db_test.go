// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versiondb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			baseDB := memdb.New()
			test(t, New(baseDB))
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	dbtest.FuzzKeyValue(f, New(memdb.New()))
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithPrefix(f, New(memdb.New()))
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, New(memdb.New()))
}

func TestIterate(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("z")
	value2 := []byte("world2")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Commit())

	iterator := db.NewIterator()
	require.NotNil(iterator)
	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key1, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())

	require.NoError(iterator.Error())

	require.NoError(db.Put(key2, value2))

	iterator = db.NewIterator()
	require.NotNil(iterator)
	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key1, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.True(iterator.Next())
	require.Equal(key2, iterator.Key())
	require.Equal(value2, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())

	require.NoError(iterator.Error())
	require.NoError(db.Delete(key1))

	iterator = db.NewIterator()
	require.NotNil(iterator)
	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key2, iterator.Key())
	require.Equal(value2, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())

	require.NoError(iterator.Error())

	require.NoError(db.Commit())
	require.NoError(db.Put(key2, value1))

	iterator = db.NewIterator()
	require.NotNil(iterator)
	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key2, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())

	require.NoError(iterator.Error())

	require.NoError(db.Commit())
	require.NoError(db.Put(key1, value2))

	iterator = db.NewIterator()
	require.NotNil(iterator)
	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key1, iterator.Key())
	require.Equal(value2, iterator.Value())

	require.True(iterator.Next())
	require.Equal(key2, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())

	require.NoError(iterator.Error())
}

func TestCommit(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	require.NoError(db.Commit())

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.Put(key1, value1))

	require.NoError(db.Commit())

	value, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, value)
	value, err = baseDB.Get(key1)
	require.NoError(err)
	require.Equal(value1, value)
}

func TestCommitClosed(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Close())
	require.Equal(database.ErrClosed, db.Commit())
}

func TestCommitClosedWrite(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	baseDB.Close()

	require.NoError(db.Put(key1, value1))
	require.Equal(database.ErrClosed, db.Commit())
}

func TestCommitClosedDelete(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")

	baseDB.Close()

	require.NoError(db.Delete(key1))
	require.Equal(database.ErrClosed, db.Commit())
}

func TestAbort(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.Put(key1, value1))

	value, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, value)
	has, err := baseDB.Has(key1)
	require.NoError(err)
	require.False(has)

	db.Abort()

	has, err = db.Has(key1)
	require.NoError(err)
	require.False(has)
	has, err = baseDB.Has(key1)
	require.NoError(err)
	require.False(has)
}

func TestCommitBatch(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.Put(key1, value1))
	has, err := baseDB.Has(key1)
	require.NoError(err)
	require.False(has)

	batch, err := db.CommitBatch()
	require.NoError(err)
	db.Abort()

	has, err = db.Has(key1)
	require.NoError(err)
	require.False(has)
	require.NoError(batch.Write())

	value, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, value)
	value, err = baseDB.Get(key1)
	require.NoError(err)
	require.Equal(value1, value)
}

func TestSetDatabase(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	newDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.SetDatabase(newDB))

	require.Equal(newDB, db.GetDatabase())

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Commit())

	has, err := baseDB.Has(key1)
	require.NoError(err)
	require.False(has)

	has, err = newDB.Has(key1)
	require.NoError(err)
	require.True(has)
}

func TestSetDatabaseClosed(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	require.NoError(db.Close())
	require.Equal(database.ErrClosed, db.SetDatabase(memdb.New()))
	require.Nil(db.GetDatabase())
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("versiondb_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				baseDB := memdb.New()
				db := New(baseDB)
				bench(b, db, keys, values)
				_ = db.Close()
			})
		}
	}
}
