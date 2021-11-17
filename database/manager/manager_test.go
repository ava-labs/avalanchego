// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

func TestNewSingleLevelDB(t *testing.T) {
	dir := t.TempDir()

	v1 := version.DefaultVersion1_0_0

	dbPath := filepath.Join(dir, v1.String())
	db, err := leveldb.New(dbPath, nil, logging.NoLog{})
	if err != nil {
		t.Fatal(err)
	}

	err = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	manager, err := NewLevelDB(dir, nil, logging.NoLog{}, v1)
	if err != nil {
		t.Fatal(err)
	}

	semDB := manager.Current()
	cmp := semDB.Version.Compare(v1)
	assert.Equal(t, 0, cmp, "incorrect version on current database")

	_, exists := manager.Previous()
	assert.False(t, exists, "there should be no previous database")

	dbs := manager.GetDatabases()
	assert.Len(t, dbs, 1)

	err = manager.Close()
	assert.NoError(t, err)
}

func TestNewCreatesSingleDB(t *testing.T) {
	dir := t.TempDir()

	v1 := version.DefaultVersion1_0_0

	manager, err := NewLevelDB(dir, nil, logging.NoLog{}, v1)
	if err != nil {
		t.Fatal(err)
	}

	semDB := manager.Current()
	cmp := semDB.Version.Compare(v1)
	assert.Equal(t, 0, cmp, "incorrect version on current database")

	_, exists := manager.Previous()
	assert.False(t, exists, "there should be no previous database")

	dbs := manager.GetDatabases()
	assert.Len(t, dbs, 1)

	err = manager.Close()
	assert.NoError(t, err)
}

func TestNewInvalidMemberPresent(t *testing.T) {
	dir := t.TempDir()

	v1 := version.NewDefaultVersion(1, 1, 0)
	v2 := version.NewDefaultVersion(1, 2, 0)

	dbPath1 := filepath.Join(dir, v1.String())
	db1, err := leveldb.New(dbPath1, nil, logging.NoLog{})
	if err != nil {
		t.Fatal(err)
	}

	dbPath2 := filepath.Join(dir, v2.String())
	db2, err := leveldb.New(dbPath2, nil, logging.NoLog{})
	if err != nil {
		t.Fatal(err)
	}

	err = db2.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewLevelDB(dir, nil, logging.NoLog{}, v2)
	assert.Error(t, err, "expected to error creating the manager due to an open db")

	err = db1.Close()
	assert.NoError(t, err)

	f, err := os.Create(filepath.Join(dir, "dummy"))
	assert.NoError(t, err)

	err = f.Close()
	assert.NoError(t, err)

	db, err := NewLevelDB(dir, nil, logging.NoLog{}, v1)
	assert.NoError(t, err, "expected not to error with a non-directory file being present")

	err = db.Close()
	assert.NoError(t, err)
}

func TestNewSortsDatabases(t *testing.T) {
	dir := t.TempDir()

	vers := []version.Version{
		version.NewDefaultVersion(2, 1, 2),
		version.NewDefaultVersion(2, 0, 2),
		version.NewDefaultVersion(1, 3, 2),
		version.NewDefaultVersion(1, 0, 2),
		version.NewDefaultVersion(1, 0, 1),
	}

	for _, version := range vers {
		dbPath := filepath.Join(dir, version.String())
		db, err := leveldb.New(dbPath, nil, logging.NoLog{})
		if err != nil {
			t.Fatal(err)
		}

		err = db.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	manager, err := NewLevelDB(dir, nil, logging.NoLog{}, vers[0])
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err = manager.Close()
		assert.NoError(t, err, "problem closing database manager")
	}()

	semDB := manager.Current()
	cmp := semDB.Version.Compare(vers[0])
	assert.Equal(t, 0, cmp, "incorrect version on current database")

	prev, exists := manager.Previous()
	if !exists {
		t.Fatal("expected to find a previous database")
	}
	cmp = prev.Version.Compare(vers[1])
	assert.Equal(t, 0, cmp, "incorrect version on previous database")

	dbs := manager.GetDatabases()
	if len(dbs) != len(vers) {
		t.Fatalf("Expected to find %d databases, but found %d", len(vers), len(dbs))
	}

	for i, db := range dbs {
		cmp = db.Version.Compare(vers[i])
		assert.Equal(t, 0, cmp, "expected to find database version %s, but found %s", vers[i], db.Version.String())
	}
}

func TestPrefixDBManager(t *testing.T) {
	db := memdb.New()

	prefix0 := []byte{0}
	db0 := prefixdb.New(prefix0, db)

	prefix1 := []byte{1}
	db1 := prefixdb.New(prefix1, db0)

	k0 := []byte{'s', 'c', 'h', 'n', 'i'}
	v0 := []byte{'t', 'z', 'e', 'l'}
	k1 := []byte{'c', 'u', 'r', 'r', 'y'}
	v1 := []byte{'w', 'u', 'r', 's', 't'}

	assert.NoError(t, db0.Put(k0, v0))
	assert.NoError(t, db1.Put(k1, v1))
	assert.NoError(t, db0.Close())
	assert.NoError(t, db1.Close())

	m := &manager{databases: []*VersionedDatabase{
		{
			Database: db,
			Version:  version.DefaultVersion1_0_0,
		},
	}}

	m0 := m.NewPrefixDBManager(prefix0)
	m1 := m0.NewPrefixDBManager(prefix1)

	val, err := m0.Current().Database.Get(k0)
	assert.NoError(t, err)
	assert.Equal(t, v0, val)

	val, err = m1.Current().Database.Get(k1)
	assert.NoError(t, err)
	assert.Equal(t, v1, val)
}

func TestNestedPrefixDBManager(t *testing.T) {
	db := memdb.New()

	prefix0 := []byte{0}
	db0 := prefixdb.NewNested(prefix0, db)

	prefix1 := []byte{1}
	db1 := prefixdb.NewNested(prefix1, db0)

	k0 := []byte{'s', 'c', 'h', 'n', 'i'}
	v0 := []byte{'t', 'z', 'e', 'l'}
	k1 := []byte{'c', 'u', 'r', 'r', 'y'}
	v1 := []byte{'w', 'u', 'r', 's', 't'}

	assert.NoError(t, db0.Put(k0, v0))
	assert.NoError(t, db1.Put(k1, v1))
	assert.NoError(t, db0.Close())
	assert.NoError(t, db1.Close())

	m := &manager{databases: []*VersionedDatabase{
		{
			Database: db,
			Version:  version.DefaultVersion1_0_0,
		},
	}}

	m0 := m.NewNestedPrefixDBManager(prefix0)
	m1 := m0.NewNestedPrefixDBManager(prefix1)

	val, err := m0.Current().Database.Get(k0)
	assert.NoError(t, err)
	assert.Equal(t, v0, val)

	val, err = m1.Current().Database.Get(k1)
	assert.NoError(t, err)
	assert.Equal(t, v1, val)
}

func TestMeterDBManager(t *testing.T) {
	registry := prometheus.NewRegistry()

	m := &manager{databases: []*VersionedDatabase{
		{
			Database: memdb.New(),
			Version:  version.NewDefaultVersion(2, 0, 0),
		},
		{
			Database: memdb.New(),
			Version:  version.NewDefaultVersion(1, 5, 0),
		},
		{
			Database: memdb.New(),
			Version:  version.DefaultVersion1_0_0,
		},
	}}

	// Create meterdb manager with fresh registry and confirm
	// that there are no errors registering metrics for multiple
	// versioned databases.
	manager, err := m.NewMeterDBManager("", registry)
	assert.NoError(t, err)

	dbs := manager.GetDatabases()
	assert.Len(t, dbs, 3)

	_, ok := dbs[0].Database.(*meterdb.Database)
	assert.True(t, ok)
	_, ok = dbs[1].Database.(*meterdb.Database)
	assert.False(t, ok)
	_, ok = dbs[2].Database.(*meterdb.Database)
	assert.False(t, ok)

	// Confirm that the error from a name conflict is handled correctly
	_, err = m.NewMeterDBManager("", registry)
	assert.Error(t, err)
}

func TestCompleteMeterDBManager(t *testing.T) {
	registry := prometheus.NewRegistry()

	m := &manager{databases: []*VersionedDatabase{
		{
			Database: memdb.New(),
			Version:  version.NewDefaultVersion(2, 0, 0),
		},
		{
			Database: memdb.New(),
			Version:  version.NewDefaultVersion(1, 5, 0),
		},
		{
			Database: memdb.New(),
			Version:  version.DefaultVersion1_0_0,
		},
	}}

	// Create complete meterdb manager with fresh registry and confirm
	// that there are no errors registering metrics for multiple
	// versioned databases.
	manager, err := m.NewCompleteMeterDBManager("", registry)
	assert.NoError(t, err)

	dbs := manager.GetDatabases()
	assert.Len(t, dbs, 3)

	_, ok := dbs[0].Database.(*meterdb.Database)
	assert.True(t, ok)
	_, ok = dbs[1].Database.(*meterdb.Database)
	assert.True(t, ok)
	_, ok = dbs[2].Database.(*meterdb.Database)
	assert.True(t, ok)

	// Confirm that the error from a name conflict is handled correctly
	_, err = m.NewCompleteMeterDBManager("", registry)
	assert.Error(t, err)
}

func TestNewManagerFromDBs(t *testing.T) {
	versions := []version.Version{
		version.NewDefaultVersion(3, 2, 0),
		version.NewDefaultVersion(1, 2, 0),
		version.NewDefaultVersion(1, 1, 1),
	}
	m, err := NewManagerFromDBs(
		[]*VersionedDatabase{
			{
				Database: memdb.New(),
				Version:  versions[2],
			},
			{
				Database: memdb.New(),
				Version:  versions[1],
			},
			{
				Database: memdb.New(),
				Version:  versions[0],
			},
		})
	assert.NoError(t, err)

	dbs := m.GetDatabases()
	assert.Len(t, dbs, len(versions))
	for i, db := range dbs {
		assert.Equal(t, 0, db.Version.Compare(versions[i]))
	}
}

func TestNewManagerFromNoDBs(t *testing.T) {
	// Should error if no dbs are given
	_, err := NewManagerFromDBs(nil)
	assert.Error(t, err)
}

func TestNewManagerFromNonUniqueDBs(t *testing.T) {
	_, err := NewManagerFromDBs(
		[]*VersionedDatabase{
			{
				Database: memdb.New(),
				Version:  version.NewDefaultVersion(1, 1, 0),
			},
			{
				Database: memdb.New(),
				Version:  version.NewDefaultVersion(1, 1, 0), // Duplicate
			},
			{
				Database: memdb.New(),
				Version:  version.NewDefaultVersion(1, 2, 0),
			},
		})
	assert.Error(t, err)
}
