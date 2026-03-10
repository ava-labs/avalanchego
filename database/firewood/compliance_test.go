// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !windows
// +build !windows

package firewood

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func newTestDB(t *testing.T) database.Database {
	tmpDir, err := os.MkdirTemp("", "firewood-compliance-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// Database compliance tests
func TestSimpleKeyValue(t *testing.T) {
	dbtest.TestSimpleKeyValue(t, newTestDB(t))
}

func TestOverwriteKeyValue(t *testing.T) {
	dbtest.TestOverwriteKeyValue(t, newTestDB(t))
}

func TestKeyEmptyValue(t *testing.T) {
	dbtest.TestKeyEmptyValue(t, newTestDB(t))
}

func TestEmptyKey(t *testing.T) {
	dbtest.TestEmptyKey(t, newTestDB(t))
}

func TestSimpleKeyValueClosed(t *testing.T) {
	dbtest.TestSimpleKeyValueClosed(t, newTestDB(t))
}

func TestMemorySafetyDatabase(t *testing.T) {
	dbtest.TestMemorySafetyDatabase(t, newTestDB(t))
}

func TestNewBatchClosed(t *testing.T) {
	dbtest.TestNewBatchClosed(t, newTestDB(t))
}

func TestBatchPut(t *testing.T) {
	dbtest.TestBatchPut(t, newTestDB(t))
}

func TestBatchDelete(t *testing.T) {
	dbtest.TestBatchDelete(t, newTestDB(t))
}

func TestMemorySafetyBatch(t *testing.T) {
	dbtest.TestMemorySafetyBatch(t, newTestDB(t))
}
