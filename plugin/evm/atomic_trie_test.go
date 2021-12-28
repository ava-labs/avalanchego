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
			writeTxs(t, repo, 0, test.lastAcceptedHeight+1, test.numTxsPerBlock, nil, operationsMap)

			// Construct the atomic trie for the first time
			atomicTrie1, err := newAtomicTrie(db, make(map[uint64]ids.ID), repo, codec, test.lastAcceptedHeight, test.commitInterval)
			if err != nil {
				t.Fatal(err)
			}
			rootHash1, commitHeight1 := atomicTrie1.LastCommitted()
			assert.EqualValues(t, test.expectedCommitHeight, commitHeight1)
			if test.expectedCommitHeight != 0 {
				assert.NotEqual(t, common.Hash{}, rootHash1)
			}

			// Verify the operations up to the expected commit height
			verifyOperations(t, atomicTrie1, codec, rootHash1, 0, test.expectedCommitHeight, operationsMap)

			// Construct the atomic trie a second time and ensure that it produces the same hash
			atomicTrie2, err := newAtomicTrie(versiondb.New(memdb.New()), make(map[uint64]ids.ID), repo, codec, test.lastAcceptedHeight, test.commitInterval)
			if err != nil {
				t.Fatal(err)
			}
			rootHash2, commitHeight2 := atomicTrie2.LastCommitted()
			assert.EqualValues(t, commitHeight1, commitHeight2)
			assert.EqualValues(t, rootHash1, rootHash2)

			// We now index additional operations up the next commit interval in order to confirm that nothing
			// during the initialization phase will cause an invalid root when indexing continues.
			nextCommitHeight := nearestCommitHeight(test.lastAcceptedHeight+test.commitInterval, test.commitInterval)
			for i := test.lastAcceptedHeight + 1; i <= nextCommitHeight; i++ {
				txs := newTestTxs(test.numTxsPerBlock)
				if err := repo.Write(i, txs); err != nil {
					t.Fatal(err)
				}

				atomicOps, err := mergeAtomicOps(txs)
				if err != nil {
					t.Fatal(err)
				}
				if err := atomicTrie1.Index(i, atomicOps); err != nil {
					t.Fatal(err)
				}
				operationsMap[i] = atomicOps
			}

			updatedRoot, updatedLastCommitHeight := atomicTrie1.LastCommitted()
			assert.EqualValues(t, nextCommitHeight, updatedLastCommitHeight)
			assert.NotEqual(t, common.Hash{}, updatedRoot)

			// Verify the operations up to the new expected commit height
			verifyOperations(t, atomicTrie1, codec, updatedRoot, 0, updatedLastCommitHeight, operationsMap)

			// Generate a new atomic trie to compare the root against.
			atomicTrie3, err := newAtomicTrie(versiondb.New(memdb.New()), make(map[uint64]ids.ID), repo, codec, nextCommitHeight, test.commitInterval)
			if err != nil {
				t.Fatal(err)
			}
			rootHash3, commitHeight3 := atomicTrie3.LastCommitted()
			assert.EqualValues(t, rootHash3, updatedRoot)
			assert.EqualValues(t, updatedLastCommitHeight, commitHeight3)
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
	atomicTrie, err := newAtomicTrie(db, make(map[uint64]ids.ID), repo, codec, lastAcceptedHeight, 10 /*commitHeightInterval*/)
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
	atomicTrie, err = newAtomicTrie(db, make(map[uint64]ids.ID), repo, codec, lastAcceptedHeight, 10 /*commitHeightInterval*/)
	assert.NoError(t, err)

	newHash, newHeight := atomicTrie.LastCommitted()
	assert.Equal(t, height, newHeight, "height should not have changed")
	assert.Equal(t, hash, newHash, "hash should be the same")
}

func newTestAtomicTrieIndexer(t *testing.T) types.AtomicTrie {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), 0)
	assert.NoError(t, err)
	indexer, err := newAtomicTrie(db, make(map[uint64]ids.ID), repo, testTxCodec(), 0, testCommitInterval)
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

func TestAtomicOpsAreNotTxOrderDependent(t *testing.T) {
	atomicTrie1 := newTestAtomicTrieIndexer(t)
	atomicTrie2 := newTestAtomicTrieIndexer(t)

	for height := uint64(0); height <= testCommitInterval; /*=205*/ height++ {
		tx1 := testDataImportTx()
		tx2 := testDataImportTx()
		atomicRequests1, err := mergeAtomicOps([]*Tx{tx1, tx2})
		assert.NoError(t, err)
		atomicRequests2, err := mergeAtomicOps([]*Tx{tx2, tx1})
		assert.NoError(t, err)

		err = atomicTrie1.Index(height, atomicRequests1)
		assert.NoError(t, err)
		err = atomicTrie2.Index(height, atomicRequests2)
		assert.NoError(t, err)
	}
	root1, height1 := atomicTrie1.LastCommitted()
	root2, height2 := atomicTrie2.LastCommitted()
	assert.NotEqual(t, common.Hash{}, root1)
	assert.Equal(t, uint64(testCommitInterval), height1)
	assert.Equal(t, uint64(testCommitInterval), height2)
	assert.Equal(t, root1, root2)
}

func TestAtomicTrieSkipsBonusBlocks(t *testing.T) {
	lastAcceptedHeight := uint64(100)
	numTxsPerBlock := 3
	commitInterval := uint64(10)
	expectedCommitHeight := uint64(100)
	db := versiondb.New(memdb.New())
	codec := testTxCodec()
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	if err != nil {
		t.Fatal(err)
	}
	operationsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	writeTxs(t, repo, 0, lastAcceptedHeight, numTxsPerBlock, nil, operationsMap)

	bonusBlocks := map[uint64]ids.ID{
		10: {},
		13: {},
		14: {},
	}
	// Construct the atomic trie for the first time
	atomicTrie, err := newAtomicTrie(db, bonusBlocks, repo, codec, lastAcceptedHeight, commitInterval)
	if err != nil {
		t.Fatal(err)
	}
	rootHash, commitHeight := atomicTrie.LastCommitted()
	assert.EqualValues(t, expectedCommitHeight, commitHeight)
	assert.NotEqual(t, common.Hash{}, rootHash)

	// Verify the operations are as expected with the bonus block heights removed from the operations map
	for height := range bonusBlocks {
		delete(operationsMap, height)
	}
	verifyOperations(t, atomicTrie, codec, rootHash, 0, expectedCommitHeight, operationsMap)
}

func TestIndexingNilShouldNotImpactTrie(t *testing.T) {
	// operations to index
	ops := make([]map[ids.ID]*atomic.Requests, 0)
	for i := 0; i <= testCommitInterval; i++ {
		ops = append(ops, testDataImportTx().mustAtomicOps())
	}

	// without nils
	a1 := newTestAtomicTrieIndexer(t)
	for i := uint64(0); i <= testCommitInterval; i++ {
		if i%2 == 0 {
			if err := a1.Index(i, ops[i]); err != nil {
				t.Fatal(err)
			}
		} else {
			// do nothing
		}
	}

	root1, height1 := a1.LastCommitted()
	assert.NotEqual(t, common.Hash{}, root1)
	assert.Equal(t, uint64(testCommitInterval), height1)

	// with nils
	a2 := newTestAtomicTrieIndexer(t)
	for i := uint64(0); i <= testCommitInterval; i++ {
		if i%2 == 0 {
			if err := a2.Index(i, ops[i]); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := a2.Index(i, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	root2, height2 := a2.LastCommitted()
	assert.NotEqual(t, common.Hash{}, root2)
	assert.Equal(t, uint64(testCommitInterval), height2)

	// key assertion of the test
	assert.Equal(t, root1, root2)
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
		atomicTrie, err = newAtomicTrie(db, make(map[uint64]ids.ID), repo, codec, lastAcceptedHeight, 5000)
		assert.NoError(b, err)

		hash, height = atomicTrie.LastCommitted()
		assert.Equal(b, lastAcceptedHeight, height)
		assert.NotEqual(b, common.Hash{}, hash)
	}
	b.StopTimer()

	// Verify operations
	verifyOperations(b, atomicTrie, codec, hash, 0, lastAcceptedHeight, operationsMap)
}
