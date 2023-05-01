// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versiondb

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		test(t, New(baseDB))
	}
}

func FuzzInterface(f *testing.F) {
	for _, test := range database.FuzzTests {
		baseDB := memdb.New()
		test(f, New(baseDB))
	}
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

	if !iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", false, true)
	} else if key := iterator.Key(); !bytes.Equal(key, key1) {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: 0x%x", key, key1)
	} else if value := iterator.Value(); !bytes.Equal(value, value1) {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: 0x%x", value, value1)
	} else if !iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", false, true)
	} else if key := iterator.Key(); !bytes.Equal(key, key2) {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: 0x%x", key, key2)
	} else if value := iterator.Value(); !bytes.Equal(value, value2) {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: 0x%x", value, value2)
	} else if iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", true, false)
	} else if key := iterator.Key(); key != nil {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: nil", key)
	} else if value := iterator.Value(); value != nil {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: nil", value)
	}

	require.NoError(iterator.Error())
	require.NoError(db.Delete(key1))

	iterator = db.NewIterator()
	if iterator == nil {
		t.Fatalf("db.NewIterator returned nil")
	}
	defer iterator.Release()

	if !iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", false, true)
	} else if key := iterator.Key(); !bytes.Equal(key, key2) {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: 0x%x", key, key2)
	} else if value := iterator.Value(); !bytes.Equal(value, value2) {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: 0x%x", value, value2)
	} else if iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", true, false)
	} else if key := iterator.Key(); key != nil {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: nil", key)
	} else if value := iterator.Value(); value != nil {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: nil", value)
	}

	require.NoError(iterator.Error())

	require.NoError(db.Commit())
	require.NoError(db.Put(key2, value1))

	iterator = db.NewIterator()
	if iterator == nil {
		t.Fatalf("db.NewIterator returned nil")
	}
	defer iterator.Release()

	if !iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", false, true)
	} else if key := iterator.Key(); !bytes.Equal(key, key2) {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: 0x%x", key, key2)
	} else if value := iterator.Value(); !bytes.Equal(value, value1) {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: 0x%x", value, value1)
	} else if iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", true, false)
	} else if key := iterator.Key(); key != nil {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: nil", key)
	} else if value := iterator.Value(); value != nil {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: nil", value)
	}
	require.NoError(iterator.Error())

	require.NoError(db.Commit())
	require.NoError(db.Put(key1, value2))

	iterator = db.NewIterator()
	if iterator == nil {
		t.Fatalf("db.NewIterator returned nil")
	}
	defer iterator.Release()

	if !iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", false, true)
	} else if key := iterator.Key(); !bytes.Equal(key, key1) {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: 0x%x", key, key1)
	} else if value := iterator.Value(); !bytes.Equal(value, value2) {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: 0x%x", value, value2)
	} else if !iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", false, true)
	} else if key := iterator.Key(); !bytes.Equal(key, key2) {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: 0x%x", key, key2)
	} else if value := iterator.Value(); !bytes.Equal(value, value1) {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: 0x%x", value, value1)
	} else if iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", true, false)
	} else if key := iterator.Key(); key != nil {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: nil", key)
	} else if value := iterator.Value(); value != nil {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: nil", value)
	}
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

	if value, err := db.Get(key1); err != nil {
		t.Fatalf("Unexpected error on db.Get: %s", err)
	} else if !bytes.Equal(value, value1) {
		t.Fatalf("db.Get Returned: 0x%x ; Expected: 0x%x", value, value1)
	} else if value, err := baseDB.Get(key1); err != nil {
		t.Fatalf("Unexpected error on db.Get: %s", err)
	} else if !bytes.Equal(value, value1) {
		t.Fatalf("db.Get Returned: 0x%x ; Expected: 0x%x", value, value1)
	}
}

func TestCommitClosed(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Close())
	if err := db.Commit(); err != database.ErrClosed {
		t.Fatalf("Expected %s on db.Commit", database.ErrClosed)
	}
}

func TestCommitClosedWrite(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	baseDB.Close()

	require.NoError(db.Put(key1, value1))
	if err := db.Commit(); err != database.ErrClosed {
		t.Fatalf("Expected %s on db.Commit", database.ErrClosed)
	}
}

func TestCommitClosedDelete(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")

	baseDB.Close()

	require.NoError(db.Delete(key1))
	if err := db.Commit(); err != database.ErrClosed {
		t.Fatalf("Expected %s on db.Commit", database.ErrClosed)
	}
}

func TestAbort(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.Put(key1, value1))

	if value, err := db.Get(key1); err != nil {
		t.Fatalf("Unexpected error on db.Get: %s", err)
	} else if !bytes.Equal(value, value1) {
		t.Fatalf("db.Get Returned: 0x%x ; Expected: 0x%x", value, value1)
	} else if has, err := baseDB.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if has {
		t.Fatalf("db.Has Returned: %v ; Expected: %v", has, false)
	}

	db.Abort()

	if has, err := db.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if has {
		t.Fatalf("db.Has Returned: %v ; Expected: %v", has, false)
	} else if has, err := baseDB.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if has {
		t.Fatalf("db.Has Returned: %v ; Expected: %v", has, false)
	}
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

	if has, err := db.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if has {
		t.Fatalf("Unexpected result of db.Has: %v", has)
	}
	require.NoError(batch.Write())

	if value, err := db.Get(key1); err != nil {
		t.Fatalf("Unexpected error on db.Get: %s", err)
	} else if !bytes.Equal(value, value1) {
		t.Fatalf("db.Get Returned: 0x%x ; Expected: 0x%x", value, value1)
	} else if value, err := baseDB.Get(key1); err != nil {
		t.Fatalf("Unexpected error on db.Get: %s", err)
	} else if !bytes.Equal(value, value1) {
		t.Fatalf("db.Get Returned: 0x%x ; Expected: 0x%x", value, value1)
	}
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
	require.ErrorIs(db.SetDatabase(memdb.New()), database.ErrClosed)
	require.Nil(db.GetDatabase())
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			baseDB := memdb.New()
			db := New(baseDB)
			bench(b, db, "versiondb", keys, values)
			_ = db.Close()
		}
	}
}
