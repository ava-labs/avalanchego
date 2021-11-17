// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versiondb

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		test(t, New(baseDB))
	}
}

func TestIterate(t *testing.T) {
	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("z")
	value2 := []byte("world2")

	if err := db.Put(key1, value1); err != nil {
		t.Fatalf("Unexpected error on db.Put: %s", err)
	}

	if err := db.Commit(); err != nil {
		t.Fatalf("Unexpected error on db.Commit: %s", err)
	}

	iterator := db.NewIterator()
	if iterator == nil {
		t.Fatalf("db.NewIterator returned nil")
	}
	defer iterator.Release()

	if !iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", false, true)
	} else if key := iterator.Key(); !bytes.Equal(key, key1) {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: 0x%x", key, key1)
	} else if value := iterator.Value(); !bytes.Equal(value, value1) {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: 0x%x", value, value1)
	} else if iterator.Next() {
		t.Fatalf("iterator.Next Returned: %v ; Expected: %v", true, false)
	} else if key := iterator.Key(); key != nil {
		t.Fatalf("iterator.Key Returned: 0x%x ; Expected: nil", key)
	} else if value := iterator.Value(); value != nil {
		t.Fatalf("iterator.Value Returned: 0x%x ; Expected: nil", value)
	} else if err := iterator.Error(); err != nil {
		t.Fatalf("iterator.Error Returned: %s ; Expected: nil", err)
	}

	if err := db.Put(key2, value2); err != nil {
		t.Fatalf("Unexpected error on database.Put: %s", err)
	}

	iterator = db.NewIterator()
	if iterator == nil {
		t.Fatalf("db.NewIterator returned nil")
	}
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
	} else if err := iterator.Error(); err != nil {
		t.Fatalf("iterator.Error Returned: %s ; Expected: nil", err)
	}

	if err := db.Delete(key1); err != nil {
		t.Fatalf("Unexpected error on database.Delete: %s", err)
	}

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
	} else if err := iterator.Error(); err != nil {
		t.Fatalf("iterator.Error Returned: %s ; Expected: nil", err)
	}

	if err := db.Commit(); err != nil {
		t.Fatalf("Unexpected error on database.Commit: %s", err)
	} else if err := db.Put(key2, value1); err != nil {
		t.Fatalf("Unexpected error on database.Put: %s", err)
	}

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
	} else if err := iterator.Error(); err != nil {
		t.Fatalf("iterator.Error Returned: %s ; Expected: nil", err)
	}

	if err := db.Commit(); err != nil {
		t.Fatalf("Unexpected error on database.Commit: %s", err)
	} else if err := db.Put(key1, value2); err != nil {
		t.Fatalf("Unexpected error on database.Put: %s", err)
	}

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
	} else if err := iterator.Error(); err != nil {
		t.Fatalf("iterator.Error Returned: %s ; Expected: nil", err)
	}
}

func TestCommit(t *testing.T) {
	baseDB := memdb.New()
	db := New(baseDB)

	if err := db.Commit(); err != nil {
		t.Fatalf("Unexpected error on db.Commit: %s", err)
	}

	key1 := []byte("hello1")
	value1 := []byte("world1")

	if err := db.Put(key1, value1); err != nil {
		t.Fatalf("Unexpected error on db.Put: %s", err)
	}

	if err := db.Commit(); err != nil {
		t.Fatalf("Unexpected error on db.Commit: %s", err)
	}

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
	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	if err := db.Put(key1, value1); err != nil {
		t.Fatalf("Unexpected error on db.Put: %s", err)
	} else if err := db.Close(); err != nil {
		t.Fatalf("Unexpected error on db.Close: %s", err)
	} else if err := db.Commit(); err != database.ErrClosed {
		t.Fatalf("Expected %s on db.Commit", database.ErrClosed)
	}
}

func TestCommitClosedWrite(t *testing.T) {
	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	baseDB.Close()

	if err := db.Put(key1, value1); err != nil {
		t.Fatalf("Unexpected error on db.Put: %s", err)
	} else if err := db.Commit(); err != database.ErrClosed {
		t.Fatalf("Expected %s on db.Commit", database.ErrClosed)
	}
}

func TestCommitClosedDelete(t *testing.T) {
	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")

	baseDB.Close()

	if err := db.Delete(key1); err != nil {
		t.Fatalf("Unexpected error on db.Delete: %s", err)
	} else if err := db.Commit(); err != database.ErrClosed {
		t.Fatalf("Expected %s on db.Commit", database.ErrClosed)
	}
}

func TestAbort(t *testing.T) {
	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	if err := db.Put(key1, value1); err != nil {
		t.Fatalf("Unexpected error on db.Put: %s", err)
	}

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
	baseDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	if err := db.Put(key1, value1); err != nil {
		t.Fatalf("Unexpected error on db.Put: %s", err)
	} else if has, err := baseDB.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if has {
		t.Fatalf("Unexpected result of db.Has: %v", has)
	}

	batch, err := db.CommitBatch()
	if err != nil {
		t.Fatalf("Unexpected error on db.CommitBatch: %s", err)
	}
	db.Abort()

	if has, err := db.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if has {
		t.Fatalf("Unexpected result of db.Has: %v", has)
	} else if err := batch.Write(); err != nil {
		t.Fatalf("Unexpected error on batch.Write: %s", err)
	}

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
	baseDB := memdb.New()
	newDB := memdb.New()
	db := New(baseDB)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	if err := db.SetDatabase(newDB); err != nil {
		t.Fatalf("Unexpected error on db.SetDatabase: %s", err)
	}

	if db.GetDatabase() != newDB {
		t.Fatalf("Unexpected database from db.GetDatabase")
	} else if err := db.Put(key1, value1); err != nil {
		t.Fatalf("Unexpected error on db.Put: %s", err)
	} else if err := db.Commit(); err != nil {
		t.Fatalf("Unexpected error on db.Commit: %s", err)
	} else if has, err := baseDB.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if has {
		t.Fatalf("db.Has Returned: %v ; Expected: %v", has, false)
	} else if has, err := newDB.Has(key1); err != nil {
		t.Fatalf("Unexpected error on db.Has: %s", err)
	} else if !has {
		t.Fatalf("db.Has Returned: %v ; Expected: %v", has, true)
	}
}

func TestSetDatabaseClosed(t *testing.T) {
	baseDB := memdb.New()
	db := New(baseDB)

	if err := db.Close(); err != nil {
		t.Fatalf("Unexpected error on db.Close: %s", err)
	} else if err := db.SetDatabase(memdb.New()); err != database.ErrClosed {
		t.Fatalf("Expected %s on db.SetDatabase", database.ErrClosed)
	} else if db.GetDatabase() != nil {
		t.Fatalf("Unexpected database from db.GetDatabase")
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			baseDB := memdb.New()
			db := New(baseDB)
			bench(b, db, "versiondb", keys, values)
			db.Close()
		}
	}
}
