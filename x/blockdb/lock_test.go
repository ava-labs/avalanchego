// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func tryOpenAt(t *testing.T, indexDir, dataDir string) (database.HeightIndex, error) {
	t.Helper()

	config := DefaultConfig().
		WithIndexDir(indexDir).
		WithDataDir(dataDir).
		WithBlockCacheSize(0)
	return New(config, logging.NoLog{})
}

// mustOpenAt opens a database with the given directories and registers a
// cleanup that closes it. Use when the open is expected to succeed and the
// database must be closed before the test ends.
func mustOpenAt(t *testing.T, indexDir, dataDir string) database.HeightIndex {
	t.Helper()

	db, err := tryOpenAt(t, indexDir, dataDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

func TestLock_RejectsOverlappingOpen(t *testing.T) {
	assertSecondOpenIsLocked := func(t *testing.T, idx1, data1, idx2, data2 string) {
		t.Helper()
		_ = mustOpenAt(t, idx1, data1)
		db, err := tryOpenAt(t, idx2, data2)
		require.Nil(t, db)
		require.ErrorIs(t, err, errDatabaseInUse)
	}

	t.Run("same directory", func(t *testing.T) {
		d := t.TempDir()
		assertSecondOpenIsLocked(t, d, d, d, d)
	})

	t.Run("shared index dir, different data dirs", func(t *testing.T) {
		idx := t.TempDir()
		assertSecondOpenIsLocked(t, idx, t.TempDir(), idx, t.TempDir())
	})

	t.Run("shared data dir, different index dirs", func(t *testing.T) {
		data := t.TempDir()
		assertSecondOpenIsLocked(t, t.TempDir(), data, t.TempDir(), data)
	})

	t.Run("same split dirs", func(t *testing.T) {
		idx, data := t.TempDir(), t.TempDir()
		assertSecondOpenIsLocked(t, idx, data, idx, data)
	})
}

func TestLock_NonOverlappingDirsBothOpen(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()

	db1 := mustOpenAt(t, rootA, rootA)
	db2 := mustOpenAt(t, rootB, rootB)
	require.NotNil(t, db1)
	require.NotNil(t, db2)
}

func TestLock_AllowsReopenAfterClose(t *testing.T) {
	dir := t.TempDir()

	db1, err := tryOpenAt(t, dir, dir)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	db2, err := tryOpenAt(t, dir, dir)
	require.NoError(t, err)
	require.NoError(t, db2.Close())
}

func TestLock_AllowsOpenAfterError(t *testing.T) {
	dir := t.TempDir()

	// Pre-write a corrupt index file so the first New() fails when loading the
	// index (an error unrelated to locking). The database should be openable
	// again once the corrupt file is removed.
	indexPath := filepath.Join(dir, indexFileName)
	require.NoError(t, os.WriteFile(indexPath, []byte("not a valid blockdb header"), 0o600))

	db, err := tryOpenAt(t, dir, dir)
	require.Nil(t, db)
	require.ErrorIs(t, err, io.EOF)

	require.NoError(t, os.Remove(indexPath))

	db2, err := tryOpenAt(t, dir, dir)
	require.NoError(t, err)
	require.NoError(t, db2.Close())
}
