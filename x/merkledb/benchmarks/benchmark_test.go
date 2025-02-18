// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

func TestGoldenDatabaseCreation(t *testing.T) {
	for _, size := range []uint64{9500, 20000, 50000, 103011} {
		testGoldenDatabaseCreation(t, size)
	}
}

func testGoldenDatabaseCreation(t *testing.T, size uint64) {
	path := getGoldenDatabaseDirectory(size)
	if Exists(getGoldenDatabaseDirectory(size)) {
		require.NoError(t, os.RemoveAll(path))
	}
	require.NoError(t, createGoldenDatabase(size))
	require.NoError(t, resetRunningDatabaseDirectory(size))
	testGoldenDatabaseContent(t, size)
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.RemoveAll(getRunningDatabaseDirectory(size)))
}

func testGoldenDatabaseContent(t *testing.T, size uint64) {
	levelDB, err := leveldb.New(
		getRunningDatabaseDirectory(size),
		getLevelDBConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	mdb, err := merkledb.New(context.Background(), levelDB, getMerkleDBConfig(nil))
	require.NoError(t, err)
	defer func() {
		mdb.Close()
		levelDB.Close()
	}()
	// read values and make sure they are what we expect them to be.
	for keyIndex := uint64(0); keyIndex < size; keyIndex++ {
		entryHash := calculateIndexEncoding(keyIndex)
		entryValue, err := mdb.Get(entryHash)
		require.NoError(t, err)
		require.Equal(t, entryHash, entryValue)
	}

	// try an entry beyond.
	entryHash := calculateIndexEncoding(size + 1)
	_, err = mdb.Get(entryHash)
	require.ErrorIs(t, err, database.ErrNotFound)

	require.NoError(t, mdb.Clear())
}
