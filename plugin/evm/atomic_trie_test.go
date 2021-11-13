package evm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/fastsync/types"
	"github.com/stretchr/testify/assert"
)

const testCommitInterval = 100

func newTestAtomicTrieIndexer(t *testing.T) types.AtomicTrie {
	db := memdb.New()
	repo := newAtomicTxRepository(db, Codec)
	indexer, err := NewBlockingAtomicTrie(Database{db}, repo)
	assert.NoError(t, err)
	assert.NotNil(t, indexer)

	{
		// for test only to make it go faaaaaasst
		atomicTrieIndexer, ok := indexer.(*blockingAtomicTrie)
		assert.True(t, ok)
		atomicTrieIndexer.commitHeightInterval = testCommitInterval
		close(atomicTrieIndexer.initialisedChan)
	}

	return indexer
}

func Test_IndexerWriteAndRead(t *testing.T) {
	indexer := newTestAtomicTrieIndexer(t)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	// process blocks and insert them into the indexer
	for height := uint64(0); height <= testCommitInterval*2+1; height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight, err := indexer.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight-1] = lastCommittedBlockHash
		}
	}

	assert.NotEqual(t, 0, lastCommittedBlockHeight)
	assert.Len(t, blockRootMap, 3)

	for height := range blockRootMap {
		root, err := indexer.Root(height)
		assert.NoError(t, err)
		assert.NotNil(t, root)
		//TODO: fix this test
		//requestsMap, err := indexer.ReadAll(root)
		//assert.NoError(t, err, "err at height %d", height)
		//assert.Equal(t, height+1, uint64(len(requestsMap)))
	}
}

func Test_IndexerInitializeFromGenesis(t *testing.T) {
	indexer := newTestAtomicTrieIndexer(t)

	// ensure last is uninitialised
	hash, height, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), height)
	assert.Equal(t, common.Hash{}, hash)

	blockAtomicOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	// process blocks and insert them into the indexer
	for height := uint64(0); height < testCommitInterval+1; height++ {
		atomicRequests := testDataExportTx().mustAtomicOps()
		blockAtomicOpsMap[height] = atomicRequests
		_, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)
	}

	hash, height, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(testCommitInterval)+1, height)
	assert.NotEqual(t, common.Hash{}, hash)
}

func Test_IndexerInitializeFromState(t *testing.T) {
	indexer := newTestAtomicTrieIndexer(t)

	// ensure last is uninitialised
	hash, height, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), height)
	assert.Equal(t, common.Hash{}, hash)

	blockRootMap := make(map[uint64]common.Hash)
	// process blocks and insert them into the indexer
	for height := uint64(0); height <= testCommitInterval; height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)

		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight, err := indexer.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	hash, height, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(testCommitInterval), height-1)
	assert.NotEqual(t, common.Hash{}, hash)

	blockAtomicOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	// process blocks and insert them into the indexer
	for height := uint64(testCommitInterval) + 1; height <= testCommitInterval*2; height++ {
		atomicRequests := testDataExportTx()
		blockAtomicOpsMap[height] = atomicRequests.mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests.mustAtomicOps())
		assert.NoError(t, err)
	}

	hash, height, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(testCommitInterval)*2+1, height)
	assert.NotEqual(t, common.Hash{}, hash)
}
