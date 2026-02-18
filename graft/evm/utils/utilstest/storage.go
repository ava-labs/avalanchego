// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utilstest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"
)

func CopyEthDB(t *testing.T, db ethdb.Database) ethdb.Database {
	newDB := rawdb.NewMemoryDatabase()
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		require.NoError(t, newDB.Put(iter.Key(), iter.Value()))
	}

	return newDB
}

// CopyDir recursively copies all files and folders from a directory [src] to a
// new temporary directory and returns the path to the new directory.
func CopyDir(t *testing.T, src string) string {
	t.Helper()

	if src == "" {
		return ""
	}

	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate the relative path from src
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode().Perm())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode().Perm())
	})

	require.NoError(t, err)
	return dst
}
