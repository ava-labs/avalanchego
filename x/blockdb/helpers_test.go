// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func newTestDatabase(t *testing.T, opts DatabaseConfig) (*Database, func()) {
	t.Helper()
	dir := t.TempDir()
	config := opts
	if config.IndexDir == "" {
		config = config.WithIndexDir(dir)
	}
	if config.DataDir == "" {
		config = config.WithDataDir(dir)
	}
	db, err := New(config, logging.NoLog{})
	require.NoError(t, err, "failed to create database")

	cleanup := func() {
		db.Close()
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

// fixedSizeBlock generates a block of the specified fixed size with height information.
func fixedSizeBlock(t *testing.T, size int, height uint64) []byte {
	require.Positive(t, size, "block size must be positive")
	b := make([]byte, size)

	// Fill the beginning with height information for better testability
	heightStr := fmt.Sprintf("block-height-%d-", height)
	if len(heightStr) <= size {
		copy(b, heightStr)
	}
	return b
}

func checkDatabaseState(t *testing.T, db *Database, maxHeight uint64, maxContiguousHeight uint64) {
	heights := db.blockHeights.Load()
	if heights != nil {
		require.Equal(t, maxHeight, heights.maxBlockHeight, "maxBlockHeight mismatch")
	} else {
		require.Equal(t, uint64(unsetHeight), maxHeight, "maxBlockHeight mismatch")
	}
	gotMCH, ok := db.MaxContiguousHeight()
	if maxContiguousHeight != unsetHeight {
		require.True(t, ok, "MaxContiguousHeight is not set, want %d", maxContiguousHeight)
		require.Equal(t, maxContiguousHeight, gotMCH, "maxContiguousHeight mismatch")
	} else {
		require.False(t, ok)
	}
}

// Helper function to create a pointer to uint64
func uint64Ptr(v uint64) *uint64 {
	return &v
}
