// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package semanticdb

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/database/prefixdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		test(t, Create(memdb.New(), nil))
		test(t, Create(memdb.New(), memdb.New()))
	}
}

func TestPrefixedDatabase(t *testing.T) {
	for _, test := range database.Tests {
		db := Create(memdb.New(), memdb.New())
		test(t, New([]byte("hello"), db))
		test(t, New([]byte("world"), db))
		test(t, New([]byte("wor"), New([]byte("ld"), db)))
		test(t, New([]byte("ld"), New([]byte("wor"), db)))
		test(t, NewNested([]byte("wor"), New([]byte("ld"), db)))
		test(t, NewNested([]byte("ld"), New([]byte("wor"), db)))
	}
}

func TestMatchesPrefixedDB(t *testing.T) {
	original := memdb.New()
	prior := memdb.New()
	sem := Create(original, prior)

	prefix1 := []byte("prefix1")
	p1 := prefixdb.New(prefix1, original)
	semP1 := New(prefix1, sem)

	k1 := []byte("hey")
	v1 := []byte("there")
	if err := p1.Put(k1, v1); err != nil {
		t.Fatal(err)
	}

	foundVal, err := semP1.Get(k1)
	if err != nil {
		t.Fatalf("Failed to get value: %s", err)
	}
	if !bytes.Equal(foundVal, v1) {
		t.Fatalf("Expected %s, but found %s", v1, foundVal)
	}

	k2 := []byte("hi")
	v2 := []byte("skipper")
	if err := semP1.Put(k2, v2); err != nil {
		t.Fatal(err)
	}
	foundVal, err = p1.Get(k2)
	if err != nil {
		t.Fatalf("Failed to get value: %s", err)
	}
	if !bytes.Equal(foundVal, v2) {
		t.Fatalf("Expected %s, but found %s", v2, foundVal)
	}

	prefix2 := []byte("prefix2")
	p2 := prefixdb.New(prefix2, p1)
	semP2 := New(prefix2, semP1)

	if err := p2.Put(k1, v1); err != nil {
		t.Fatal(err)
	}

	foundVal, err = semP2.Get(k1)
	if err != nil {
		t.Fatalf("Failed to get value: %s", err)
	}
	if !bytes.Equal(foundVal, v1) {
		t.Fatalf("Expected %s, but found %s", v1, foundVal)
	}

	if err := semP2.Put(k2, v2); err != nil {
		t.Fatal(err)
	}
	foundVal, err = p2.Get(k2)
	if err != nil {
		t.Fatalf("Failed to get value: %s", err)
	}
	if !bytes.Equal(foundVal, v2) {
		t.Fatalf("Expected %s, but found %s", v2, foundVal)
	}
}

func TestCreateFromPath(t *testing.T) {
	dir, err := ioutil.TempDir("", "temp-database-dir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	major := 1
	minor := 0
	patch := 0
	db1, err := CreateFromPath(dir, major, minor, patch)
	if err != nil {
		t.Fatalf("Failed to create database: %s", err)
	}
	// If this errors, it can be safely ignored.
	defer db1.Close()

	if _, ok := db1.(*Database); ok {
		t.Fatal("Should not have created semanticdb when no prior version was present")
	}
	if err := db1.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := CreateFromPath(dir, major, minor+2, patch+3)
	if err != nil {
		t.Fatalf("Failed to create second database: %s", err)
	}
	// If this errors, it can be safely ignored.
	defer db2.Close()

	if _, ok := db2.(*Database); !ok {
		t.Fatalf("Should have created semanticdb when prior version was present")
	}
	if err := db2.Close(); err != nil {
		t.Fatal(err)
	}
}
