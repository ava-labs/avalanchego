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
			lastCommittedBlockHash, lastCommittedBlockHeight, err = indexer.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	// ensure we have 3 roots
	assert.Len(t, blockRootMap, 3)

	hash, height, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.EqualValues(t, lastCommittedBlockHeight, height, "expected %d was %d", 200, lastCommittedBlockHeight)
	assert.Equal(t, lastCommittedBlockHash, hash)

	// verify all roots are there
	for height, hash := range blockRootMap {
		root, err := indexer.Root(height)
		assert.NoError(t, err)
		assert.Equal(t, hash, root)
	}

	// ensure Index does not accept blocks older than last committed height
	_, err = indexer.Index(10, testDataExportTx().mustAtomicOps())
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

	hash, height, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(testCommitInterval)*2, height)
	assert.NotEqual(t, common.Hash{}, hash)
}
