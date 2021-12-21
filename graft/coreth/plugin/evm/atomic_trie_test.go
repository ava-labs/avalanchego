// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/statesync/types"
)

const testCommitInterval = 100

func (tx *Tx) mustAtomicOps() map[ids.ID]*atomic.Requests {
	id, reqs, err := tx.AtomicOps()
	if err != nil {
		panic(err)
	}
	return map[ids.ID]*atomic.Requests{id: reqs}
}

func TestNearestCommitHeight(t *testing.T) {
	type test struct {
		height, commitInterval, expectedCommitHeight uint64
	}

	for _, test := range []test{
		{
			height:               4500,
			commitInterval:       4096,
			expectedCommitHeight: 4096,
		},
		{
			height:               8500,
			commitInterval:       4096,
			expectedCommitHeight: 8192,
		},
		{
			height:               950,
			commitInterval:       100,
			expectedCommitHeight: 900,
		},
	} {
		commitHeight := nearestCommitHeight(test.height, test.commitInterval)
		assert.Equal(t, commitHeight, test.expectedCommitHeight)
	}
}

func TestAtomicTrieInitialize(t *testing.T) {
	type test struct {
		commitInterval, lastAcceptedHeight, expectedCommitHeight uint64
		numTxsPerBlock                                           int
	}
	for name, test := range map[string]test{
		"genesis": {
			commitInterval:       10,
			lastAcceptedHeight:   0,
			expectedCommitHeight: 0,
			numTxsPerBlock:       0,
		},
		"before first commit": {
			commitInterval:       10,
			lastAcceptedHeight:   5,
			expectedCommitHeight: 0,
			numTxsPerBlock:       3,
		},
		"first commit": {
			commitInterval:       10,
			lastAcceptedHeight:   10,
			expectedCommitHeight: 10,
			numTxsPerBlock:       3,
		},
		"past first commit": {
			commitInterval:       10,
			lastAcceptedHeight:   15,
			expectedCommitHeight: 10,
			numTxsPerBlock:       3,
		},
		"many existing commits": {
			commitInterval:       10,
			lastAcceptedHeight:   1000,
			expectedCommitHeight: 1000,
			numTxsPerBlock:       3,
		},
		"many existing commits plus 1": {
			commitInterval:       10,
			lastAcceptedHeight:   1001,
			expectedCommitHeight: 1000,
			numTxsPerBlock:       3,
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := versiondb.New(memdb.New())
			codec := testTxCodec()
			repo, err := NewAtomicTxRepository(db, codec, test.lastAcceptedHeight)
			if err != nil {
				t.Fatal(err)
			}
			operationsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
			writeTxs(t, repo, 0, test.lastAcceptedHeight, test.numTxsPerBlock, nil, operationsMap)

			// Construct the atomic trie for the first time
			atomicTrie1, err := newAtomicTrie(db, repo, codec, test.lastAcceptedHeight, test.commitInterval)
			if err != nil {
				t.Fatal(err)
			}
			rootHash1, commitHeight1 := atomicTrie1.LastCommitted()
			assert.EqualValues(t, test.expectedCommitHeight, commitHeight1)
			if test.expectedCommitHeight != 0 {
				assert.NotEqual(t, common.Hash{}, rootHash1)
			}

			// Verify the operations are as expected
			verifyOperations(t, atomicTrie1, codec, rootHash1, operationsMap, int(test.expectedCommitHeight)*test.numTxsPerBlock)

			freshDB := versiondb.New(memdb.New())
			// Construct the atomic trie a second time and ensure that it produces the same hash
			atomicTrie2, err := newAtomicTrie(freshDB, repo, codec, test.lastAcceptedHeight, test.commitInterval)
			if err != nil {
				t.Fatal(err)
			}
			rootHash2, commitHeight2 := atomicTrie2.LastCommitted()
			assert.EqualValues(t, commitHeight1, commitHeight2)
			assert.EqualValues(t, rootHash1, rootHash2)
		})
	}
}

func TestIndexerInitializesOnlyOnce(t *testing.T) {
	lastAcceptedHeight := uint64(25)
	db := versiondb.New(memdb.New())
	codec := testTxCodec()
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(t, err)
	operationsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	writeTxs(t, repo, 0, lastAcceptedHeight+1, 2, nil, operationsMap)

	// Initialize atomic repository
	atomicTrie, err := newAtomicTrie(db, repo, codec, lastAcceptedHeight, 10 /*commitHeightInterval*/)
	assert.NoError(t, err)

	hash, height := atomicTrie.LastCommitted()
	assert.NotEqual(t, common.Hash{}, hash)
	assert.Equal(t, uint64(20), height)

	// We write another tx at a height below the last committed height in the repo and then
	// re-initialize the atomic trie since initialize is not supposed to run again the height
	// at the trie should still be the old height with the old commit hash without any changes.
	// This scenario is not realistic, but is used to test potential double initialization behavior.
	err = repo.Write(15, []*Tx{testDataExportTx()})
	assert.NoError(t, err)

	// Re-initialize the atomic trie
	atomicTrie, err = newAtomicTrie(db, repo, codec, lastAcceptedHeight, 10 /*commitHeightInterval*/)
	assert.NoError(t, err)

	newHash, newHeight := atomicTrie.LastCommitted()
	assert.Equal(t, height, newHeight, "height should not have changed")
	assert.Equal(t, hash, newHash, "hash should be the same")
}

func newTestAtomicTrieIndexer(t *testing.T) types.AtomicTrie {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), 0)
	assert.NoError(t, err)
	indexer, err := newAtomicTrie(db, repo, testTxCodec(), 0, testCommitInterval)
	assert.NoError(t, err)
	assert.NotNil(t, indexer)
	return indexer
}

func TestIndexerWriteAndRead(t *testing.T) {
	atomicTrie := newTestAtomicTrieIndexer(t)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	var lastCommittedBlockHash common.Hash

	// process 205 blocks so that we get three commits (0, 100, 200)
	for height := uint64(0); height <= testCommitInterval*2+5; /*=205*/ height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		err := atomicTrie.Index(height, atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight = atomicTrie.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	// ensure we have 3 roots
	assert.Len(t, blockRootMap, 3)

	hash, height := atomicTrie.LastCommitted()
	assert.EqualValues(t, lastCommittedBlockHeight, height, "expected %d was %d", 200, lastCommittedBlockHeight)
	assert.Equal(t, lastCommittedBlockHash, hash)

	// Verify that [atomicTrie] can access each of the expected roots
	for height, hash := range blockRootMap {
		root, err := atomicTrie.Root(height)
		assert.NoError(t, err)
		assert.Equal(t, hash, root)
	}

	// Ensure that Index refuses to accept blocks older than the last committed height
	err := atomicTrie.Index(10, testDataExportTx().mustAtomicOps())
	assert.Error(t, err)
	assert.Equal(t, "height 10 must be after last committed height 200", err.Error())

	// Ensure Index does not accept blocks beyond the next commit interval
	nextCommitHeight := lastCommittedBlockHeight + testCommitInterval + 1 // =301
	err = atomicTrie.Index(nextCommitHeight, testDataExportTx().mustAtomicOps())
	assert.Error(t, err)
	assert.Equal(t, "height 301 not within the next commit height 300", err.Error())
}

func BenchmarkAtomicTrieInit(b *testing.B) {
	db := versiondb.New(memdb.New())
	codec := testTxCodec()

	operationsMap := make(map[uint64]map[ids.ID]*atomic.Requests)

	lastAcceptedHeight := uint64(25000)
	// add 25000 * 3 = 75000 transactions
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(b, err)
	writeTxs(b, repo, 0, 25000, 3, nil, operationsMap)

	var atomicTrie types.AtomicTrie
	var hash common.Hash
	var height uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		atomicTrie, err = newAtomicTrie(db, repo, codec, lastAcceptedHeight, 5000)
		assert.NoError(b, err)

		hash, height = atomicTrie.LastCommitted()
		assert.Equal(b, lastAcceptedHeight, height)
		assert.NotEqual(b, common.Hash{}, hash)
	}
	b.StopTimer()

	// Verify operations
	verifyOperations(b, atomicTrie, codec, hash, operationsMap, 75000)
}

// TODO add bonus block test
