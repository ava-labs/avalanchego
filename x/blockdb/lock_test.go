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
	db, err := New(config, logging.NoLog{})
	if err != nil {
		return db, err
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db, nil
}

func TestLock_DirectoryOverlap(t *testing.T) {
	type dbDirs struct {
		index string
		data  string
	}

	tests := []struct {
		name    string
		first   dbDirs
		second  dbDirs
		wantErr error
	}{
		{
			name:    "same",
			first:   dbDirs{index: "same", data: "same"},
			second:  dbDirs{index: "same", data: "same"},
			wantErr: errDatabaseInUse,
		},
		{
			name:    "same_index",
			first:   dbDirs{index: "index", data: "data1"},
			second:  dbDirs{index: "index", data: "data2"},
			wantErr: errDatabaseInUse,
		},
		{
			name:    "same_data",
			first:   dbDirs{index: "index1", data: "data"},
			second:  dbDirs{index: "index2", data: "data"},
			wantErr: errDatabaseInUse,
		},
		{
			name:    "same_split_dirs",
			first:   dbDirs{index: "index", data: "data"},
			second:  dbDirs{index: "index", data: "data"},
			wantErr: errDatabaseInUse,
		},
		{
			name:   "different_single_dirs",
			first:  dbDirs{index: "db1", data: "db1"},
			second: dbDirs{index: "db2", data: "db2"},
		},
		{
			name:   "different_split_dirs",
			first:  dbDirs{index: "index1", data: "data1"},
			second: dbDirs{index: "index2", data: "data2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			index1 := filepath.Join(dir, test.first.index)
			data1 := filepath.Join(dir, test.first.data)
			_, err := tryOpenAt(t, index1, data1)
			require.NoError(t, err)

			index2 := filepath.Join(dir, test.second.index)
			data2 := filepath.Join(dir, test.second.data)
			_, err = tryOpenAt(t, index2, data2)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestLock_AllowsReopenAfterClose(t *testing.T) {
	dir := t.TempDir()

	db1, err := tryOpenAt(t, dir, dir)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	_, err = tryOpenAt(t, dir, dir)
	require.NoError(t, err)
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

	_, err = tryOpenAt(t, dir, dir)
	require.NoError(t, err)
}
