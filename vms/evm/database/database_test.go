// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"

	avalanchegodb "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
)

// databaseAdapter adapts our ethdb.KeyValueStore to the avalanchego database.Database interface
type databaseAdapter struct {
	ethdb.KeyValueStore
	baseDB avalanchegodb.Database
}

func (d *databaseAdapter) NewBatch() avalanchegodb.Batch {
	return d.baseDB.NewBatch()
}

func (d *databaseAdapter) NewIterator() avalanchegodb.Iterator {
	return d.baseDB.NewIterator()
}

func (d *databaseAdapter) NewIteratorWithStart(start []byte) avalanchegodb.Iterator {
	return d.baseDB.NewIteratorWithStart(start)
}

func (d *databaseAdapter) NewIteratorWithPrefix(prefix []byte) avalanchegodb.Iterator {
	return d.baseDB.NewIteratorWithPrefix(prefix)
}

func (d *databaseAdapter) NewIteratorWithStartAndPrefix(start, prefix []byte) avalanchegodb.Iterator {
	return d.baseDB.NewIteratorWithStartAndPrefix(start, prefix)
}

func (d *databaseAdapter) Compact(start, limit []byte) error {
	return d.baseDB.Compact(start, limit)
}

func (d *databaseAdapter) HealthCheck(ctx context.Context) (interface{}, error) {
	return d.baseDB.HealthCheck(ctx)
}

func TestInterface(t *testing.T) {
	for name, test := range dbtest.TestsBasic {
		t.Run(name, func(t *testing.T) {
			baseDB := memdb.New()
			test(t, New(baseDB))
		})
	}
}

func TestInterfaceFull(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			baseDB := memdb.New()
			wrappedDB := New(baseDB)
			testDB := &testDatabase{database: wrappedDB.(database)}

			// Create adapter to satisfy database.Database interface
			adapter := &databaseAdapter{
				KeyValueStore: testDB,
				baseDB:        baseDB,
			}
			test(t, adapter)
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	baseDB := memdb.New()
	wrappedDB := New(baseDB)
	testDB := &testDatabase{database: wrappedDB.(database)}
	adapter := &databaseAdapter{
		KeyValueStore: testDB,
		baseDB:        baseDB,
	}
	dbtest.FuzzKeyValue(f, adapter)
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	baseDB := memdb.New()
	wrappedDB := New(baseDB)
	testDB := &testDatabase{database: wrappedDB.(database)}
	adapter := &databaseAdapter{
		KeyValueStore: testDB,
		baseDB:        baseDB,
	}
	dbtest.FuzzNewIteratorWithPrefix(f, adapter)
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	baseDB := memdb.New()
	wrappedDB := New(baseDB)
	testDB := &testDatabase{database: wrappedDB.(database)}
	adapter := &databaseAdapter{
		KeyValueStore: testDB,
		baseDB:        baseDB,
	}
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, adapter)
}

func TestProductionErrors(t *testing.T) {
	baseDB := memdb.New()
	wrappedDB := New(baseDB)

	t.Run("NewSnapshot_ReturnsError", func(t *testing.T) {
		_, err := wrappedDB.NewSnapshot()
		require.ErrorIs(t, err, ErrSnapshotNotSupported)
	})

	t.Run("Stat_ReturnsError", func(t *testing.T) {
		_, err := wrappedDB.Stat("test")
		require.ErrorIs(t, err, ErrStatNotSupported)
	})
}
