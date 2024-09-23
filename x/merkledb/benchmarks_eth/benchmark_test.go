// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"
	"path"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
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

func TestRevisions(t *testing.T) {
	revisionCount := uint64(10)
	wd, _ := os.Getwd()
	dbPath := path.Join(wd, "db-test-revisions")
	require.NoError(t, os.RemoveAll(dbPath))
	err := os.Mkdir(dbPath, 0o777)
	require.NoError(t, err)
	ldb, err := rawdb.NewLevelDBDatabase(dbPath, levelDBCacheSize, 200, "metrics_prefix", false)
	require.NoError(t, err)
	trieDb := triedb.NewDatabase(ldb, &triedb.Config{
		Preimages: false,
		IsVerkle:  false,
		HashDB:    nil,
		PathDB: &pathdb.Config{
			StateHistory:   revisionCount, // keep the same requiremenet across each db
			CleanCacheSize: 1024 * 1024,
			DirtyCacheSize: 1024 * 1024,
		},
	})
	tdb := trie.NewEmpty(trieDb)
	for entryIdx := uint64(0); entryIdx < 1000000; entryIdx++ {
		entryHash := calculateIndexEncoding(entryIdx)
		tdb.Update(entryHash, make([]byte, 32))
	}

	revisions := make([]common.Hash, 0)
	revisionsTrie := make([]*trie.Trie, 0)

	var nodes *trienode.NodeSet
	var root common.Hash
	root, nodes = tdb.Commit(false)
	err = trieDb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil /*states*/)
	require.NoError(t, err)
	err = trieDb.Commit(root, false)
	require.NoError(t, err)
	tdb2, err := trie.New(trie.TrieID(root), trieDb)
	require.NoError(t, err)
	revisionsTrie = append(revisionsTrie, tdb2)
	tdb, err = trie.New(trie.TrieID(root), trieDb)
	require.NoError(t, err)
	revisions = append(revisions, root)

	for currentRev := uint64(1); currentRev < 1000; currentRev++ {
		// update the tree based on the current revision.
		for entryIdx := uint64(currentRev-1) * 1000; entryIdx < uint64(currentRev)*1000; entryIdx++ {
			entryHash := calculateIndexEncoding(entryIdx)
			tdb.Update(entryHash, entryHash)
		}
		root, nodes = tdb.Commit(false)
		err = trieDb.Update(root, revisions[len(revisions)-1], currentRev, trienode.NewWithNodeSet(nodes), nil /*states*/)
		require.NoError(t, err)
		err = trieDb.Commit(root, false)
		require.NoError(t, err)
		tdb2, err = trie.New(trie.TrieID(root), trieDb)
		require.NoError(t, err)
		revisionsTrie = append(revisionsTrie, tdb2)
		tdb, err = trie.New(trie.TrieID(root), trieDb)
		require.NoError(t, err)
		revisions = append(revisions, root)

		// check all the historical revision and see which ones are available and which one aren't.
		for revIdx, revTrie := range revisionsTrie {
			entryHash := calculateIndexEncoding(uint64(revIdx) * 1000)
			entryValue, err := revTrie.Get(entryHash)
			if revIdx != 0 {
				require.NoError(t, err)
				require.Equal(t, make([]byte, 32), entryValue)
			} else {
				require.Error(t, err) // should be "missing node error"
				continue
			}

			entryHash = calculateIndexEncoding(uint64(revIdx)*1000 - 1)
			entryValue, err = revTrie.Get(entryHash)
			require.NoError(t, err)
			require.Equal(t, entryHash, entryValue)
		}
	}
}
