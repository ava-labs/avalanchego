// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func newDatabase(t *testing.T, config DatabaseConfig) *Database {
	t.Helper()

	db := newHeightIndexDatabase(t, config.WithBlockCacheSize(0))
	require.IsType(t, &Database{}, db)
	return db.(*Database)
}

func newHeightIndexDatabase(t *testing.T, config DatabaseConfig) database.HeightIndex {
	t.Helper()

	dir := t.TempDir()
	if config.IndexDir == "" {
		config = config.WithIndexDir(dir)
	}
	if config.DataDir == "" {
		config = config.WithDataDir(dir)
	}
	db, err := New(config, logging.NoLog{})
	require.NoError(t, err)
	return db
}

func newCacheDatabase(t *testing.T, config DatabaseConfig) *cacheDB {
	t.Helper()

	db := newHeightIndexDatabase(t, config)
	require.IsType(t, &cacheDB{}, db)
	return db.(*cacheDB)
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

func checkDatabaseState(t *testing.T, db *Database, maxHeight uint64) {
	t.Helper()

	actualMaxHeight := db.maxBlockHeight.Load()
	require.Equal(t, maxHeight, actualMaxHeight, "maxBlockHeight mismatch")
}
