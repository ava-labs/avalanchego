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
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

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
	writeTxs(t, repo, 0, lastAcceptedHeight, 3, nil, operationsMap)

	// create an atomic trie
	// on create it will initialize all the transactions from the above atomic repository
	atomicTrie1, err := newAtomicTrie(db, repo, codec, lastAcceptedHeight, 100)
	assert.NoError(t, err)

	lastCommittedHash1, lastCommittedHeight1 := atomicTrie1.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash1)
	assert.EqualValues(t, 1000, lastCommittedHeight1)

	verifyOperations(t, atomicTrie1, codec, lastCommittedHash1, operationsMap, 3000)

	// iterate on a new atomic trie to make sure there is no resident state affecting the data and the
	// iterator
	atomicTrie2, err := NewAtomicTrie(db, repo, codec, lastAcceptedHeight)
	assert.NoError(t, err)
	lastCommittedHash2, lastCommittedHeight2 := atomicTrie2.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash2)
	assert.EqualValues(t, 1000, lastCommittedHeight2)

	verifyOperations(t, atomicTrie2, codec, lastCommittedHash1, operationsMap, 3000)
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
	writeTxs(t, repo, 0, lastAcceptedHeight, 3, nil, operationsMap)

	// create an atomic trie
	// on create it will initialize all the transactions from the above atomic repository
	atomicTrie, err := newAtomicTrie(db, repo, codec, lastAcceptedHeight, 100)
	assert.NoError(t, err)

	lastCommittedHash, lastCommittedHeight := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash)
	assert.EqualValues(t, 1000, lastCommittedHeight)

	verifyOperations(t, atomicTrie, codec, lastCommittedHash, operationsMap, 3000)

	assert.NoError(t, atomicTrie.trie.TryUpdate(utils.RandomBytes(50), utils.RandomBytes(50)))
	assert.NoError(t, atomicTrie.commit(lastCommittedHeight+1))
	corruptedHash, _ := atomicTrie.LastCommitted()
	iter, err := atomicTrie.Iterator(corruptedHash, 0)
	assert.NoError(t, err)
	for iter.Next() {
	}
	assert.Error(t, iter.Error())
}
