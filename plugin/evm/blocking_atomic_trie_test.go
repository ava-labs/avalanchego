// (c) 2020-2021, Ava Labs, Inc.
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

	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/fastsync/types"
)

const testCommitInterval = 100

func (tx *Tx) mustAtomicOps() map[ids.ID]*atomic.Requests {
	id, reqs, err := tx.Accept()
	if err != nil {
		panic(err)
	}
	return map[ids.ID]*atomic.Requests{id: reqs}
}

func Test_nearestCommitHeight(t *testing.T) {
	blockNumber := uint64(7029687)
	height := nearestCommitHeight(blockNumber, 4096)
	assert.Less(t, blockNumber, height+4096)
}

func Test_BlockingAtomicTrie_InitializeGenesis(t *testing.T) {
	lastAcceptedHeight := uint64(0)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	tx := testDataImportTx()
	err = repo.Write(0, []*Tx{tx})
	assert.NoError(t, err)

	atomicTrieDB := memorydb.New()
	atomicTrie, err := NewBlockingAtomicTrie(atomicTrieDB, repo, testTxCodec())
	assert.NoError(t, err)
	atomicTrie.(*blockingAtomicTrie).commitHeightInterval = 10
	dbCommitFn := func() error {
		return nil
	}
	err = atomicTrie.Initialize(lastAcceptedHeight, dbCommitFn)
	assert.NoError(t, err)

	_, num := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.EqualValues(t, 0, num)
}

func Test_BlockingAtomicTrie_InitializeGenesisPlusOne(t *testing.T) {
	lastAcceptedHeight := uint64(1)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	err = repo.Write(0, []*Tx{testDataImportTx()})
	assert.NoError(t, err)
	err = repo.Write(1, []*Tx{testDataImportTx()})
	assert.NoError(t, err)

	atomicTrieDB := memorydb.New()
	atomicTrie, err := NewBlockingAtomicTrie(atomicTrieDB, repo, testTxCodec())
	assert.NoError(t, err)
	atomicTrie.(*blockingAtomicTrie).commitHeightInterval = 10
	dbCommitFn := func() error {
		return nil
	}
	err = atomicTrie.Initialize(lastAcceptedHeight, dbCommitFn)
	assert.NoError(t, err)

	_, num := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.EqualValues(t, 0, num, "expected %d was %d", 0, num)
}

func Test_BlockingAtomicTrie_Initialize(t *testing.T) {
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	// create state
	for i := uint64(0); i <= lastAcceptedHeight; i++ {
		err := repo.Write(i, []*Tx{testDataExportTx()})
		assert.NoError(t, err)
	}

	atomicTrieDB := memorydb.New()
	atomicTrie, err := NewBlockingAtomicTrie(atomicTrieDB, repo, testTxCodec())
	atomicTrie.(*blockingAtomicTrie).commitHeightInterval = 10
	assert.NoError(t, err)
	dbCommitFn := func() error {
		return nil
	}

	err = atomicTrie.Initialize(lastAcceptedHeight, dbCommitFn)
	assert.NoError(t, err)

	lastCommittedHash, lastCommittedHeight := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash)
	assert.EqualValues(t, 1000, lastCommittedHeight, "expected %d but was %d", 1000, lastCommittedHeight)

	atomicTrie, err = NewBlockingAtomicTrie(atomicTrieDB, repo, testTxCodec())
	assert.NoError(t, err)
	iterator, err := atomicTrie.Iterator(lastCommittedHash, 0)
	assert.NoError(t, err)
	entriesIterated := uint64(0)
	for iterator.Next() {
		assert.Greater(t, len(iterator.AtomicOps()), 0)
		assert.NoError(t, iterator.Error())
		entriesIterated++
	}
	assert.NoError(t, iterator.Error())
	assert.EqualValues(t, 1001, entriesIterated, "expected %d was %d", 1001, entriesIterated)
}

func newTestAtomicTrieIndexer(t *testing.T) types.AtomicTrie {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), 0)
	assert.NoError(t, err)
	indexer, err := NewBlockingAtomicTrie(Database{db}, repo, testTxCodec())
	assert.NoError(t, err)
	assert.NotNil(t, indexer)

	{
		// for test only to make it go faaaaaasst
		atomicTrieIndexer, ok := indexer.(*blockingAtomicTrie)
		assert.True(t, ok)
		atomicTrieIndexer.commitHeightInterval = testCommitInterval
	}

	return indexer
}

func Test_IndexerWriteAndRead(t *testing.T) {
	indexer := newTestAtomicTrieIndexer(t)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	var lastCommittedBlockHash common.Hash

	// process 205 blocks so that we get three commits (0, 100, 200)
	for height := uint64(0); height <= testCommitInterval*2+5; /*=205*/ height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight = indexer.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	// ensure we have 3 roots
	assert.Len(t, blockRootMap, 3)

	hash, height := indexer.LastCommitted()
	assert.EqualValues(t, lastCommittedBlockHeight, height, "expected %d was %d", 200, lastCommittedBlockHeight)
	assert.Equal(t, lastCommittedBlockHash, hash)

	// verify all roots are there
	for height, hash := range blockRootMap {
		root, err := indexer.Root(height)
		assert.NoError(t, err)
		assert.Equal(t, hash, root)
	}

	// ensure Index does not accept blocks older than last committed height
	_, err := indexer.Index(10, testDataExportTx().mustAtomicOps())
	assert.Error(t, err)
	assert.Equal(t, "height 10 must be after last committed height 200", err.Error())

	// ensure Index does not accept blocks beyond next committed height
	nextCommitHeight := lastCommittedBlockHeight + testCommitInterval + 1 // =301
	_, err = indexer.Index(nextCommitHeight, testDataExportTx().mustAtomicOps())
	assert.Error(t, err)
	assert.Equal(t, "height 301 not within the next commit height 300", err.Error())
}

func Test_IndexerInitializeFromState(t *testing.T) {
	indexer := newTestAtomicTrieIndexer(t)

	// ensure last is uninitialised
	hash, height := indexer.LastCommitted()
	assert.Equal(t, uint64(0), height)
	assert.Equal(t, common.Hash{}, hash)

	blockRootMap := make(map[uint64]common.Hash)
	// process blocks and insert them into the indexer
	for height := uint64(0); height <= testCommitInterval; height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)

		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight := indexer.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	hash, height = indexer.LastCommitted()
	assert.Equal(t, uint64(testCommitInterval), height)
	assert.NotEqual(t, common.Hash{}, hash)

	blockAtomicOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	// process blocks and insert them into the indexer
	for height := uint64(testCommitInterval) + 1; height <= testCommitInterval*2; height++ {
		atomicRequests := testDataExportTx()
		blockAtomicOpsMap[height] = atomicRequests.mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests.mustAtomicOps())
		assert.NoError(t, err)
	}

	hash, height = indexer.LastCommitted()
	assert.Equal(t, uint64(testCommitInterval)*2, height)
	assert.NotEqual(t, common.Hash{}, hash)
}

func Test_IndexerInitializesOnlyOnce(t *testing.T) {
	lastAcceptedHeight := uint64(100)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	for i := uint64(0); i < lastAcceptedHeight; i++ {
		err := repo.Write(i, []*Tx{testDataImportTx(), testDataExportTx()})
		assert.NoError(t, err)
	}

	// initialize atomic repository
	indexer, err := NewBlockingAtomicTrie(Database{db}, repo, testTxCodec())
	assert.NoError(t, err)
	{
		indexer.(*blockingAtomicTrie).commitHeightInterval = 90
	}

	err = indexer.Initialize(lastAcceptedHeight, nil)
	assert.NoError(t, err)

	hash, height := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, hash)
	assert.Equal(t, uint64(90), height)

	// the following scenario is not realistic but is there to test double initialize behaviour
	err = repo.Write(101, []*Tx{testDataExportTx()})
	assert.NoError(t, err)

	// we wrote another tx at new height in the repo and we then re-initialise the atomic trie
	// since initialize is not supposed to run again the height at the trie will still be the
	// old height with the old commit hash without any changes

	// initialize atomic indexer again
	indexer, err = NewBlockingAtomicTrie(Database{db}, repo, testTxCodec())
	assert.NoError(t, err)
	{
		indexer.(*blockingAtomicTrie).commitHeightInterval = 90
	}

	err = indexer.Initialize(lastAcceptedHeight, nil) // should be skipped
	assert.NoError(t, err)

	newHash, height := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(90), height, "height should not have changed")
	assert.Equal(t, hash, newHash, "hash should be the same")
}

func Test_IndexerInitializeProducesSameHashEveryTime(t *testing.T) {
	t.Parallel()
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	txsPerHeight := 30
	for i := uint64(0); i < lastAcceptedHeight; i++ {
		txs := make([]*Tx, txsPerHeight)
		for j := 0; j+1 < txsPerHeight; j += 2 {
			txs[j] = testDataExportTx()
			txs[j+1] = testDataImportTx()
		}
		err := repo.Write(i, txs)
		assert.NoError(t, err)
	}

	// initialize atomic repository
	indexer, err := NewBlockingAtomicTrie(Database{db}, repo, testTxCodec())
	assert.NoError(t, err)
	{
		indexer.(*blockingAtomicTrie).commitHeightInterval = 90
	}

	err = indexer.Initialize(lastAcceptedHeight, nil)
	assert.NoError(t, err)

	expectedCommitHeight := nearestCommitHeight(lastAcceptedHeight, 90)
	hash, indexerHeight := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, hash)
	assert.Equal(t, expectedCommitHeight, indexerHeight)

	// we create another atomic trie, intializing it with the same dataset
	initializeRuns := 50
	for i := 0; i < initializeRuns; i++ {
		// initialize atomic indexer again
		indexer, err = NewBlockingAtomicTrie(Database{memdb.New()}, repo, testTxCodec())
		assert.NoError(t, err)
		{
			indexer.(*blockingAtomicTrie).commitHeightInterval = 90
		}

		// run initialize
		err = indexer.Initialize(lastAcceptedHeight, nil)
		assert.NoError(t, err)

		// expects heights and hashes to be exactly the same
		newHash, indexerHeight := indexer.LastCommitted()
		assert.NoError(t, err)
		assert.Equal(t, expectedCommitHeight, indexerHeight, "height should not have changed")
		assert.Equal(t, hash, newHash, "hash should be the same")
	}
}
