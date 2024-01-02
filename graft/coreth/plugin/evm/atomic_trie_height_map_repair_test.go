// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestAtomicTrieRepairHeightMap(t *testing.T) {
	for name, test := range map[string]testAtomicTrieRepairHeightMap{
		"last accepted after commit interval": {
			lastAccepted:  3*testCommitInterval + 5,
			skipAtomicTxs: func(height uint64) bool { return false },
		},
		"last accepted exactly a commit interval": {
			lastAccepted:  3 * testCommitInterval,
			skipAtomicTxs: func(height uint64) bool { return false },
		},
		"no atomic txs in a commit interval": {
			lastAccepted:  3 * testCommitInterval,
			skipAtomicTxs: func(height uint64) bool { return height > testCommitInterval && height <= 2*testCommitInterval },
		},
		"no atomic txs in the most recent commit intervals": {
			lastAccepted:  3 * testCommitInterval,
			skipAtomicTxs: func(height uint64) bool { return height > testCommitInterval+1 },
		},
	} {
		t.Run(name, func(t *testing.T) { test.run(t) })
	}
}

type testAtomicTrieRepairHeightMap struct {
	lastAccepted  uint64
	skipAtomicTxs func(height uint64) bool
}

func (test testAtomicTrieRepairHeightMap) run(t *testing.T) {
	require := require.New(t)

	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), 0, nil)
	require.NoError(err)
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, 0, common.Hash{}, testCommitInterval)
	require.NoError(err)
	atomicTrie := atomicBackend.AtomicTrie().(*atomicTrie)

	heightMap := make(map[uint64]common.Hash)
	for height := uint64(1); height <= test.lastAccepted; height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		if test.skipAtomicTxs(height) {
			atomicRequests = nil
		}
		err := indexAtomicTxs(atomicTrie, height, atomicRequests)
		require.NoError(err)
		if height%testCommitInterval == 0 {
			root, _ := atomicTrie.LastCommitted()
			heightMap[height] = root
		}
	}

	// Verify that [atomicTrie] can access each of the expected roots
	verifyRoots := func(expectZero bool) {
		for height, hash := range heightMap {
			root, err := atomicTrie.Root(height)
			require.NoError(err)
			if expectZero {
				require.Zero(root)
			} else {
				require.Equal(hash, root)
			}
		}
	}
	verifyRoots(false)

	// destroy the height map
	for height := range heightMap {
		err := atomicTrie.metadataDB.Delete(database.PackUInt64(height))
		require.NoError(err)
	}
	require.NoError(db.Commit())
	verifyRoots(true)

	// repair the height map
	repaired, err := atomicTrie.RepairHeightMap(test.lastAccepted)
	require.NoError(err)
	verifyRoots(false)
	require.True(repaired)

	// partially destroy the height map
	_, lastHeight := atomicTrie.LastCommitted()
	err = atomicTrie.metadataDB.Delete(database.PackUInt64(lastHeight))
	require.NoError(err)
	err = atomicTrie.metadataDB.Put(
		heightMapRepairKey,
		database.PackUInt64(lastHeight-testCommitInterval),
	)
	require.NoError(err)

	// repair the height map
	repaired, err = atomicTrie.RepairHeightMap(test.lastAccepted)
	require.NoError(err)
	verifyRoots(false)
	require.True(repaired)

	// try to repair the height map again
	repaired, err = atomicTrie.RepairHeightMap(test.lastAccepted)
	require.NoError(err)
	require.False(repaired)
}
