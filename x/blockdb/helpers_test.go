// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"crypto/rand"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func newTestDatabase(t *testing.T, syncToDisk bool, opts *DatabaseConfig) (*Database, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "blockdb_test_*")
	require.NoError(t, err, "failed to create temp dir")
	idxDir := filepath.Join(dir, "idx")
	dataDir := filepath.Join(dir, "dat")
	var config DatabaseConfig
	if opts != nil {
		config = *opts
	} else {
		config = DefaultDatabaseConfig()
	}
	db, err := New(idxDir, dataDir, syncToDisk, true, config, logging.NoLog{})
	if err != nil {
		os.RemoveAll(dir)
		require.NoError(t, err, "failed to create database")
	}
	cleanup := func() {
		db.Close()
		os.RemoveAll(dir)
	}
	return db, cleanup
}

// randomBlock generates a random block of size 1KB-50KB.
func randomBlock(t *testing.T) []byte {
	size, err := rand.Int(rand.Reader, big.NewInt(50*1024-1024+1))
	require.NoError(t, err, "failed to generate random size")
	blockSize := int(size.Int64()) + 1024 // 1KB to 50KB
	b := make([]byte, blockSize)
	_, err = rand.Read(b)
	require.NoError(t, err, "failed to fill random block")
	return b
}

func checkDatabaseState(t *testing.T, db *Database, maxHeight uint64, maxContiguousHeight uint64) {
	require.Equal(t, maxHeight, db.maxBlockHeight.Load(), "maxBlockHeight mismatch")
	gotMCH, ok := db.MaxContiguousHeight()
	if maxContiguousHeight != unsetHeight {
		require.True(t, ok, "MaxContiguousHeight is not set, want %d", maxContiguousHeight)
		require.Equal(t, maxContiguousHeight, gotMCH, "maxContiguousHeight mismatch")
	}
}
