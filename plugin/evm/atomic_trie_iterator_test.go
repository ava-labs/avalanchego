// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSharedMemory() avalancheatomic.SharedMemory {
	m := avalancheatomic.NewMemory(memdb.New())
	return m.NewSharedMemory(testCChainID)
}

func TestIteratorCanIterate(t *testing.T) {
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(t, err)

	// create state with multiple transactions
	// since each test transaction generates random ID for blockchainID we should get
	// multiple blockchain IDs per block in the overall combined atomic operation map
	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	// create an atomic trie
	// on create it will initialize all the transactions from the above atomic repository
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 100)
	assert.NoError(t, err)
	atomicTrie1 := atomicBackend.AtomicTrie()

	lastCommittedHash1, lastCommittedHeight1 := atomicTrie1.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash1)
	assert.EqualValues(t, 1000, lastCommittedHeight1)

	verifyOperations(t, atomicTrie1, codec, lastCommittedHash1, 1, 1000, operationsMap)

	// iterate on a new atomic trie to make sure there is no resident state affecting the data and the
	// iterator
	atomicBackend2, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 100)
	assert.NoError(t, err)
	atomicTrie2 := atomicBackend2.AtomicTrie()
	lastCommittedHash2, lastCommittedHeight2 := atomicTrie2.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash2)
	assert.EqualValues(t, 1000, lastCommittedHeight2)

	verifyOperations(t, atomicTrie2, codec, lastCommittedHash1, 1, 1000, operationsMap)
}

func TestIteratorHandlesInvalidData(t *testing.T) {
	require := require.New(t)
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	require.NoError(err)

	// create state with multiple transactions
	// since each test transaction generates random ID for blockchainID we should get
	// multiple blockchain IDs per block in the overall combined atomic operation map
	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	// create an atomic trie
	// on create it will initialize all the transactions from the above atomic repository
	commitInterval := uint64(100)
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, commitInterval)
	require.NoError(err)
	atomicTrie := atomicBackend.AtomicTrie()

	lastCommittedHash, lastCommittedHeight := atomicTrie.LastCommitted()
	require.NoError(err)
	require.NotEqual(common.Hash{}, lastCommittedHash)
	require.EqualValues(1000, lastCommittedHeight)

	verifyOperations(t, atomicTrie, codec, lastCommittedHash, 1, 1000, operationsMap)

	// Add a random key-value pair to the atomic trie in order to test that the iterator correctly
	// handles an error when it runs into an unexpected key-value pair in the trie.
	atomicTrieSnapshot, err := atomicTrie.OpenTrie(lastCommittedHash)
	require.NoError(err)
	require.NoError(atomicTrieSnapshot.Update(utils.RandomBytes(50), utils.RandomBytes(50)))

	nextRoot, nodes, err := atomicTrieSnapshot.Commit(false)
	require.NoError(err)
	err = atomicTrie.InsertTrie(nodes, nextRoot)
	require.NoError(err)
	isCommit, err := atomicTrie.AcceptTrie(lastCommittedHeight+commitInterval, nextRoot)
	require.NoError(err)
	require.True(isCommit)

	corruptedHash, _ := atomicTrie.LastCommitted()
	iter, err := atomicTrie.Iterator(corruptedHash, nil)
	require.NoError(err)
	for iter.Next() {
	}
	require.Error(iter.Error())
}
