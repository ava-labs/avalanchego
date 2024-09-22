// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"
	"path"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/stretchr/testify/require"
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
	resetRunningDatabaseDirectory(size)
	testGoldenDatabaseContent(t, size)
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.RemoveAll(getRunningDatabaseDirectory(size)))
}

func testGoldenDatabaseContent(t *testing.T, size uint64) {
	rootBytes, err := os.ReadFile(path.Join(getRunningDatabaseDirectory(size), "root.txt"))
	require.NoError(t, err)

	ldb, err := rawdb.Open(rawdb.OpenOptions{
		Type:              "leveldb",
		Directory:         getRunningDatabaseDirectory(size),
		AncientsDirectory: "",
		Namespace:         "metrics_prefix",
		Cache:             levelDBCacheSize,
		Handles:           200,
		ReadOnly:          false,
		Ephemeral:         false,
	})
	require.NoError(t, err)

	trieDb := triedb.NewDatabase(ldb, &triedb.Config{
		Preimages: false,
		IsVerkle:  false,
		HashDB:    nil,
		PathDB:    getPathDBConfig(),
	})

	parentHash := common.BytesToHash(rootBytes)
	tdb, err := trie.New(trie.TrieID(parentHash), trieDb)
	require.NoError(t, err)

	// read values and make sure they are what we expect them to be.
	for keyIndex := uint64(0); keyIndex < size; keyIndex++ {
		entryHash := calculateIndexEncoding(keyIndex)
		entryValue, err := tdb.Get(entryHash)
		require.NoError(t, err)
		require.Equal(t, entryHash, entryValue)
	}

	// try an entry beyond.
	entryHash := calculateIndexEncoding(size + 1)
	entryValue, err := tdb.Get(entryHash)
	require.NoError(t, err) // for geth, we have no error in case of a missing key.
	require.Equal(t, []byte(nil), entryValue)

	require.NoError(t, trieDb.Close())
	require.NoError(t, ldb.Close())
}
