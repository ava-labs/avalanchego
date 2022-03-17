// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func testSharedMemory() atomic.SharedMemory {
	m := &atomic.Memory{}
	m.Initialize(logging.NoLog{}, memdb.New())
	return m.NewSharedMemory(testCChainID)
}

func TestIteratorCanIterate(t *testing.T) {
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	codec := testTxCodec()
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(t, err)

	// create state with multiple transactions
	// since each test transaction generates random ID for blockchainID we should get
	// multiple blockchain IDs per block in the overall combined atomic operation map
	operationsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	// create an atomic trie
	// on create it will initialize all the transactions from the above atomic repository
	atomicTrie1, err := newAtomicTrie(db, testSharedMemory(), nil, repo, codec, lastAcceptedHeight, 100)
	assert.NoError(t, err)

	lastCommittedHash1, lastCommittedHeight1 := atomicTrie1.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash1)
	assert.EqualValues(t, 1000, lastCommittedHeight1)

	verifyOperations(t, atomicTrie1, codec, lastCommittedHash1, 1, 1000, operationsMap)

	// iterate on a new atomic trie to make sure there is no resident state affecting the data and the
	// iterator
	atomicTrie2, err := newAtomicTrie(db, testSharedMemory(), nil, repo, codec, lastAcceptedHeight, 100)
	assert.NoError(t, err)
	lastCommittedHash2, lastCommittedHeight2 := atomicTrie2.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash2)
	assert.EqualValues(t, 1000, lastCommittedHeight2)

	verifyOperations(t, atomicTrie2, codec, lastCommittedHash1, 1, 1000, operationsMap)
}

func TestIteratorHandlesInvalidData(t *testing.T) {
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	codec := testTxCodec()
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(t, err)

	// create state with multiple transactions
	// since each test transaction generates random ID for blockchainID we should get
	// multiple blockchain IDs per block in the overall combined atomic operation map
	operationsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	// create an atomic trie
	// on create it will initialize all the transactions from the above atomic repository
	atomicTrie, err := newAtomicTrie(db, testSharedMemory(), nil, repo, codec, lastAcceptedHeight, 100)
	assert.NoError(t, err)

	lastCommittedHash, lastCommittedHeight := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash)
	assert.EqualValues(t, 1000, lastCommittedHeight)

	verifyOperations(t, atomicTrie, codec, lastCommittedHash, 1, 1000, operationsMap)

	// Add a random key-value pair to the atomic trie in order to test that the iterator correctly
	// handles an error when it runs into an unexpected key-value pair in the trie.
	assert.NoError(t, atomicTrie.trie.TryUpdate(utils.RandomBytes(50), utils.RandomBytes(50)))
	assert.NoError(t, atomicTrie.commit(lastCommittedHeight+1))
	corruptedHash, _ := atomicTrie.LastCommitted()
	iter, err := atomicTrie.Iterator(corruptedHash, nil)
	assert.NoError(t, err)
	for iter.Next() {
	}
	assert.Error(t, iter.Error())
}
