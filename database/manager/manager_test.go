// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"os"
	"path"
	"testing"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestCreateManagerSingleDB(t *testing.T) {
	dir := t.TempDir()

	v1 := version.NewDefaultVersion(1, 0, 0)

	dbPath := path.Join(dir, v1.String())
	db, err := leveldb.New(dbPath, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	err = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	manager, err := CreateManager(dir, v1)
	if err != nil {
		t.Fatal(err)
	}

	semDB := manager.Current()
	cmp := semDB.Compare(v1)
	assert.Equal(t, 0, cmp, "incorrect version on current database")

	_, exists := manager.Last()
	assert.False(t, exists, "there should be no previous database")

	dbs := manager.GetDatabases()
	assert.Len(t, dbs, 1)

	err = manager.Close()
	assert.NoError(t, err)
}

func TestCreateManagerCreatesSingleDB(t *testing.T) {
	dir := t.TempDir()

	v1 := version.NewDefaultVersion(1, 0, 0)

	manager, err := CreateManager(dir, v1)
	if err != nil {
		t.Fatal(err)
	}

	semDB := manager.Current()
	cmp := semDB.Compare(v1)
	assert.Equal(t, 0, cmp, "incorrect version on current database")

	_, exists := manager.Last()
	assert.False(t, exists, "there should be no previous database")

	dbs := manager.GetDatabases()
	assert.Len(t, dbs, 1)

	err = manager.Close()
	assert.NoError(t, err)
}

func TestCreateManagerInvalidMemberPresent(t *testing.T) {
	dir := t.TempDir()

	v1 := version.NewDefaultVersion(1, 1, 0)
	v2 := version.NewDefaultVersion(1, 2, 0)

	dbPath1 := path.Join(dir, v1.String())
	db1, err := leveldb.New(dbPath1, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	dbPath2 := path.Join(dir, v2.String())
	db2, err := leveldb.New(dbPath2, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	err = db2.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = CreateManager(dir, v1)
	assert.Error(t, err, "expected to error creating the manager due to open db")

	err = db1.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = os.Create(path.Join(dir, "dummy"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = CreateManager(dir, v1)
	assert.Error(t, err, "expected to error due to non-directory file being present")
}

func TestCreateManagerSortsDatabases(t *testing.T) {
	dir := t.TempDir()

	vers := []version.Version{
		version.NewDefaultVersion(2, 1, 2),
		version.NewDefaultVersion(2, 0, 2),
		version.NewDefaultVersion(1, 3, 2),
		version.NewDefaultVersion(1, 0, 2),
		version.NewDefaultVersion(1, 0, 1),
	}

	for _, version := range vers {
		dbPath := path.Join(dir, version.String())
		db, err := leveldb.New(dbPath, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}

		err = db.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	manager, err := CreateManager(dir, vers[0])
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err = manager.Close()
		assert.NoError(t, err, "problem closing database manager")
	}()

	semDB := manager.Current()
	cmp := semDB.Compare(vers[0])
	assert.Equal(t, 0, cmp, "incorrect version on current database")

	prev, exists := manager.Last()
	if !exists {
		t.Fatal("expected to find a previous database")
	}
	cmp = prev.Compare(vers[1])
	assert.Equal(t, 0, cmp, "incorrect version on previous database")

	dbs := manager.GetDatabases()
	if len(dbs) != len(vers) {
		t.Fatalf("Expected to find %d databases, but found %d", len(vers), len(dbs))
	}

	for i, db := range dbs {
		cmp = db.Compare(vers[i])
		assert.Equal(t, 0, cmp, "expected to find database version %s, but found %s", vers[i], db.Version.String())
	}
}

// Test that prefix/meter work correctly
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

	m := &manager{databases: []*SemanticDatabase{
		{
			Database: db,
			Version:  version.NewDefaultVersion(1, 0, 0),
		},
	}}

	m0 := m.NewPrefixDBManager(prefix0)
	m1 := m0.NewPrefixDBManager(prefix1)

	val, err := m0.Current().Get(k0)
	assert.NoError(t, err)
	assert.Equal(t, v0, val)

	val, err = m1.Current().Get(k1)
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

	m := &manager{databases: []*SemanticDatabase{
		{
			Database: db,
			Version:  version.NewDefaultVersion(1, 0, 0),
		},
	}}

	m0 := m.NewNestedPrefixDBManager(prefix0)
	m1 := m0.NewNestedPrefixDBManager(prefix1)

	val, err := m0.Current().Get(k0)
	assert.NoError(t, err)
	assert.Equal(t, v0, val)

	val, err = m1.Current().Get(k1)
	assert.NoError(t, err)
	assert.Equal(t, v1, val)
}

func TestMeterDBManager(t *testing.T) {
	registry := prometheus.NewRegistry()

	m := &manager{databases: []*SemanticDatabase{
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
			Version:  version.NewDefaultVersion(1, 0, 0),
		},
	}}

	_, err := m.NewMeterDBManager("", registry)
	assert.NoError(t, err)
}
