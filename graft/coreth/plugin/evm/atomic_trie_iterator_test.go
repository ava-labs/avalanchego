package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestIteratorCanIterate(t *testing.T) {
	lastAcceptedHeight := uint64(1000)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)

	// create state with multiple transactions
	// since each test transaction generates random ID for blockchainID we should get
	// multiple blockchain IDs per block in the overall combined atomic operation map
	for i := uint64(0); i <= lastAcceptedHeight; i++ {
		err := repo.Write(i, []*Tx{testDataExportTx(), testDataImportTx(), testDataExportTx()})
		assert.NoError(t, err)
	}

	// create atomic trie
	// on create it will initialize all the transactions from the above atomic repository
	atomicTrie, err := newBlockingAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight, 100)
	assert.NoError(t, err)

	lastCommittedHash, lastCommittedHeight := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash)
	assert.EqualValues(t, 1000, lastCommittedHeight, "expected %d but was %d", 1000, lastCommittedHeight)

	// we iterate on a new blocking atomic trie to make sure there is no resident state affecting the data and the
	// iterator
	atomicTrie, err = NewBlockingAtomicTrie(db, repo, testTxCodec(), lastAcceptedHeight)
	assert.NoError(t, err)

	iterator, err := atomicTrie.Iterator(lastCommittedHash, 0)
	assert.NoError(t, err)
	entriesIterated := uint64(0)
	for iterator.Next() {
		assert.NotNil(t, iterator.AtomicOps())
		assert.NoError(t, iterator.Error())
		entriesIterated++
	}
	assert.NoError(t, iterator.Error())

	// we assert 3003 values because the iterator iterates by height+blockchainID
	// so if for a given height there are atomic operations belonging to 3 blockchainIDs then the
	// atomic trie iterator will iterate 3 times for the same height.
	assert.EqualValues(t, 3003, entriesIterated, "expected %d was %d", 1001, entriesIterated)
}
