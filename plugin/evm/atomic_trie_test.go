// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/database/prefixdb"

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
	blockNumber := uint64(7029687)
	height := nearestCommitHeight(blockNumber, 4096)
	assert.Less(t, uint64(7028736), height+4096)
}

func TestAtomicTrieInitializeGenesis(t *testing.T) {
	lastAcceptedHeight := uint64(0)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	tx := testDataImportTx()
	err = repo.Write(0, []*Tx{tx})
	assert.NoError(t, err)

	atomicTrie, err := newAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 10 /*commitHeightInterval*/)
	assert.NoError(t, err)

	_, num := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.EqualValues(t, 0, num)
}

func TestAtomicTrieInitializeGenesisPlusOne(t *testing.T) {
	lastAcceptedHeight := uint64(1)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	err = repo.Write(0, []*Tx{testDataImportTx()})
	assert.NoError(t, err)
	err = repo.Write(1, []*Tx{testDataImportTx()})
	assert.NoError(t, err)

	atomicTrie, err := newAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 10 /*commitHeightInterval*/)
	assert.NoError(t, err)

	_, num := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.EqualValues(t, 0, num, "expected %d was %d", 0, num)
}

func TestAtomicTrieInitialize(t *testing.T) {
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	// create state
	for i := uint64(0); i <= lastAcceptedHeight; i++ {
		err := repo.Write(i, []*Tx{testDataExportTx()})
		assert.NoError(t, err)
	}

	atomicTrie, err := newAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 10 /*commitHeightInterval*/)
	assert.NoError(t, err)

	lastCommittedHash, lastCommittedHeight := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash)
	assert.EqualValues(t, 1000, lastCommittedHeight, "expected %d but was %d", 1000, lastCommittedHeight)

	atomicTrie, err = NewAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	it, err := atomicTrie.Iterator(lastCommittedHash, 0)
	assert.NoError(t, err)
	entriesIterated := uint64(0)
	for it.Next() {
		assert.NotNil(t, it.AtomicOps())
		assert.NoError(t, it.Error())
		entriesIterated++
	}
	assert.NoError(t, it.Error())
	assert.EqualValues(t, 1001, entriesIterated, "expected %d was %d", 1001, entriesIterated)
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

func TestIndexerInitializeFromState(t *testing.T) {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), 0)
	assert.NoError(t, err)

	lastAcceptedHeight := uint64(1000)
	actualOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	for height := uint64(0); height < lastAcceptedHeight; height++ {
		// 3 transactions per height
		txs := []*Tx{testDataImportTx(), testDataExportTx(), testDataImportTx()}
		actualOpsMap[height], err = mergeAtomicOps(txs)
		assert.NoError(t, err)
		err = repo.Write(height, txs)
		assert.NoError(t, err)
	}

	indexer, err := newAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 100)
	assert.NoError(t, err)
	assert.NotNil(t, indexer)

	hash, height := indexer.LastCommitted()
	assert.Equal(t, lastAcceptedHeight, height)
	assert.NotEqual(t, common.Hash{}, hash)

	atomicIter, err := indexer.Iterator(hash, 0)
	assert.NoError(t, err)
	opsCount := 0
	for atomicIter.Next() {
		chainOps := actualOpsMap[atomicIter.BlockNumber()][atomicIter.BlockchainID()]
		putRequests := atomicIter.AtomicOps().PutRequests
		removeRequests := atomicIter.AtomicOps().RemoveRequests

		assert.Equal(t, chainOps.PutRequests, putRequests)
		assert.Equal(t, chainOps.RemoveRequests, removeRequests)
		opsCount++
	}
	assert.Equal(t, 3000, opsCount)
}

func BenchmarkEncodeDecode(b *testing.B) {
	codec := testTxCodec()
	tx := testDataImportTx()
	_, requests, err := tx.AtomicOps()
	assert.NoError(b, err)

	rlpAtomicBytes, err := rlp.EncodeToBytes(requests)
	assert.NoError(b, err)
	assert.Greater(b, len(rlpAtomicBytes), 1)

	codecAtomicBytes, err := codec.Marshal(codecVersion, requests)
	assert.NoError(b, err)
	assert.Greater(b, len(codecAtomicBytes), 1)

	fmt.Printf("rlpLen: %d, codecLen: %d\n", len(rlpAtomicBytes), len(codecAtomicBytes))

	b.ReportAllocs()
	b.ResetTimer()
	benchmarks := []struct {
		name string
		fn   func()
	}{
		{
			name: "RLP encode",
			fn: func() {
				_, _ = rlp.EncodeToBytes(requests)
			},
		}, {
			name: "RLP decode",
			fn: func() {
				var decode atomic.Requests
				_ = rlp.DecodeBytes(rlpAtomicBytes, &decode)
			},
		}, {
			name: "codec encode",
			fn: func() {
				_, _ = codec.Marshal(codecVersion, requests)
			},
		}, {
			name: "codec decode",
			fn: func() {
				var decode atomic.Requests
				_, _ = codec.Unmarshal(codecAtomicBytes, &decode)
			},
		},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				bm.fn()
			}
		})
	}
}

func TestIndexerInitializesOnlyOnce(t *testing.T) {
	lastAcceptedHeight := uint64(100)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
	for i := uint64(0); i < lastAcceptedHeight; i++ {
		err := repo.Write(i, []*Tx{testDataImportTx(), testDataExportTx()})
		assert.NoError(t, err)
	}

	// initialize atomic repository
	indexer, err := newAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 90 /*commitHeightInterval*/)
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
	indexer, err = newAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 90 /*commitHeightInterval*/)
	assert.NoError(t, err)

	newHash, height := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(90), height, "height should not have changed")
	assert.Equal(t, hash, newHash, "hash should be the same")
}

func TestAtomicTrieInitializeProducesSameHashEveryTime(t *testing.T) {
	t.Parallel()
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)
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
	indexer, err := newAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 90 /*commitHeightInterval*/)
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
		indexer, err = newAtomicTrie(versiondb.New(memdb.New()), repo, testTxCodec(), lastAcceptedHeight, 90 /*commitHeightInterval*/)
		assert.NoError(t, err)
		// expects heights and hashes to be exactly the same
		newHash, indexerHeight := indexer.LastCommitted()
		assert.NoError(t, err)
		assert.Equal(t, expectedCommitHeight, indexerHeight, "height should not have changed")
		assert.Equal(t, hash, newHash, "hash should be the same")
	}
}

func BenchmarkAtomicTrieInit(b *testing.B) {
	db := versiondb.New(memdb.New())
	codec := testTxCodec()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)

	lastAcceptedHeight := uint64(25000)
	// add 25000 * 3 = 75000 transactions
	addTxs(b, codec, acceptedAtomicTxDB, 0, lastAcceptedHeight, 3, txMap)
	err := db.Commit()
	assert.NoError(b, err)

	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(b, err)

	var atomicTrie types.AtomicTrie
	var hash common.Hash
	var height uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		atomicTrie, err = newAtomicTrie(versiondb.New(memdb.New()), repo, codec, lastAcceptedHeight, 5000)
		assert.NoError(b, err)

		hash, height = atomicTrie.LastCommitted()
		assert.Equal(b, lastAcceptedHeight, height)
		assert.NotEqual(b, common.Hash{}, hash)
	}
	b.StopTimer()

	// verify ops
	it, err := atomicTrie.Iterator(hash, 0)
	assert.NoError(b, err)
	ops := 0
	for it.Next() {
		ops++
	}
	assert.Equal(b, 75000, ops)
}
