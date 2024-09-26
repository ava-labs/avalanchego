// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"
	"path"
	"testing"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/stretchr/testify/require"
)

var testPathDbConfig = pathdb.Config{
	CleanCacheSize: units.MiB, // use a small cache for testing
	DirtyCacheSize: 0,         // force changes to disk
	ReadOnly:       false,     // allow writes
}

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
		Cache:             levelDBCacheSizeMB,
		Handles:           200,
		ReadOnly:          false,
		Ephemeral:         false,
	})
	require.NoError(t, err)

	trieDb := triedb.NewDatabase(ldb, &triedb.Config{
		Preimages: false,
		IsVerkle:  false,
		HashDB:    nil,
		PathDB:    &testPathDbConfig,
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

func TestRevisions(t *testing.T) {
	require := require.New(t)

	// Clean up the database directory
	wd, _ := os.Getwd()
	dbPath := path.Join(wd, "db-test-revisions")
	require.NoError(os.RemoveAll(dbPath))
	require.NoError(os.Mkdir(dbPath, 0o777))

	// Create a new leveldb database
	ldb, err := rawdb.NewLevelDBDatabase(dbPath, levelDBCacheSizeMB, 200, "metrics_prefix", false)
	require.NoError(err)

	// Create a new trie database
	const maxDiffs = 128 // triedb will store 128 diffs.
	trieDB := triedb.NewDatabase(
		ldb,
		&triedb.Config{
			Preimages: false,
			IsVerkle:  false,
			HashDB:    nil,
			PathDB:    &testPathDbConfig,
		},
	)

	// Initially populate the trie
	const diffSize = 100
	var (
		index uint64
		tdb   = trie.NewEmpty(trieDB)
	)
	for i := 0; i < diffSize; i++ {
		entryHash := calculateIndexEncoding(index)
		require.NoError(tdb.Update(entryHash, entryHash))
		index++
	}

	// Commit the initial state
	root, nodes := tdb.Commit(false)
	require.NoError(trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil /*states*/))

	const numDiffs = 150
	rootHashes := []common.Hash{root}
	for height := uint64(1); height < numDiffs; height++ {
		prevRoot := root
		tdb, err = trie.New(trie.TrieID(prevRoot), trieDB)
		require.NoError(err)

		// update the tree based on the current revision.
		for i := 0; i < diffSize; i++ {
			entryHash := calculateIndexEncoding(index)
			require.NoError(tdb.Update(entryHash, entryHash))
			index++
		}

		// Commit the changes
		root, nodes = tdb.Commit(false)
		require.NoError(trieDB.Update(root, prevRoot, height, trienode.NewWithNodeSet(nodes), nil /*states*/))

		rootHashes = append(rootHashes, root)
	}

	// Verify that old revisions were pruned.
	numPruned := max(len(rootHashes)-maxDiffs-1, 0)
	for _, root := range rootHashes[:numPruned] {
		_, err = trie.New(trie.TrieID(root), trieDB)
		require.Error(err)
	}

	// Verify that all 128 revisions are queryable and return the expected data.
	for height, root := range rootHashes[numPruned:] {
		height += numPruned

		tdb, err = trie.New(trie.TrieID(root), trieDB)
		require.NoError(err)

		maxIndex := diffSize * uint64(height+1)
		for i := uint64(0); i < maxIndex; i++ {
			entryHash := calculateIndexEncoding(i)
			entryValue, err := tdb.Get(entryHash)
			require.NoError(err)
			require.Equal(entryHash, entryValue)
		}

		entryHash := calculateIndexEncoding(maxIndex)
		entryValue, err := tdb.Get(entryHash)
		require.NoError(err)
		require.Empty(entryValue)
	}

	trieDB = triedb.NewDatabase(
		ldb,
		&triedb.Config{
			Preimages: false,
			IsVerkle:  false,
			HashDB:    nil,
			PathDB: &pathdb.Config{
				CleanCacheSize: 1024 * 1024,
				DirtyCacheSize: 0, // Disable dirty cache to force trieDB to write to disk.
			},
		},
	)

	_, err = trieDB.Reader(rootHashes[numPruned])
	require.NoError(err)
}
