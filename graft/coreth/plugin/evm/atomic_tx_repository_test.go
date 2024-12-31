// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"testing"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

// addTxs writes [txsPerHeight] txs for heights ranging in [fromHeight, toHeight) directly to [acceptedAtomicTxDB],
// storing the resulting transactions in [txMap] if non-nil and the resulting atomic operations in [operationsMap]
// if non-nil.
func addTxs(t testing.TB, codec codec.Manager, acceptedAtomicTxDB database.Database, fromHeight uint64, toHeight uint64, txsPerHeight int, txMap map[uint64][]*atomic.Tx, operationsMap map[uint64]map[ids.ID]*avalancheatomic.Requests) {
	for height := fromHeight; height < toHeight; height++ {
		txs := make([]*atomic.Tx, 0, txsPerHeight)
		for i := 0; i < txsPerHeight; i++ {
			tx := atomic.NewTestTx()
			txs = append(txs, tx)
			txBytes, err := codec.Marshal(atomic.CodecVersion, tx)
			assert.NoError(t, err)

			// Write atomic transactions to the [acceptedAtomicTxDB]
			// in the format handled prior to the migration to the atomic
			// tx repository.
			packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
			packer.PackLong(height)
			packer.PackBytes(txBytes)
			txID := tx.ID()
			err = acceptedAtomicTxDB.Put(txID[:], packer.Bytes)
			assert.NoError(t, err)
		}
		// save this to the map (if non-nil) for verifying expected results in verifyTxs
		if txMap != nil {
			txMap[height] = txs
		}
		if operationsMap != nil {
			atomicRequests, err := mergeAtomicOps(txs)
			if err != nil {
				t.Fatal(err)
			}
			operationsMap[height] = atomicRequests
		}
	}
}

// constTxsPerHeight returns a function for passing to [writeTxs], which will return a constant number
// as the number of atomic txs per height to create.
func constTxsPerHeight(txCount int) func(uint64) int {
	return func(uint64) int { return txCount }
}

// writeTxs writes [txsPerHeight] txs for heights ranging in [fromHeight, toHeight) through the Write call on [repo],
// storing the resulting transactions in [txMap] if non-nil and the resulting atomic operations in [operationsMap]
// if non-nil.
func writeTxs(t testing.TB, repo AtomicTxRepository, fromHeight uint64, toHeight uint64,
	txsPerHeight func(height uint64) int, txMap map[uint64][]*atomic.Tx, operationsMap map[uint64]map[ids.ID]*avalancheatomic.Requests,
) {
	for height := fromHeight; height < toHeight; height++ {
		txs := atomic.NewTestTxs(txsPerHeight(height))
		if err := repo.Write(height, txs); err != nil {
			t.Fatal(err)
		}
		// save this to the map (if non-nil) for verifying expected results in verifyTxs
		if txMap != nil {
			txMap[height] = txs
		}
		if operationsMap != nil {
			atomicRequests, err := mergeAtomicOps(txs)
			if err != nil {
				t.Fatal(err)
			}
			if len(atomicRequests) == 0 {
				continue
			}
			operationsMap[height] = atomicRequests
		}
	}
}

// verifyTxs asserts [repo] can find all txs in [txMap] by height and txID
func verifyTxs(t testing.TB, repo AtomicTxRepository, txMap map[uint64][]*atomic.Tx) {
	// We should be able to fetch indexed txs by height:
	for height, expectedTxs := range txMap {
		txs, err := repo.GetByHeight(height)
		assert.NoErrorf(t, err, "unexpected error on GetByHeight at height=%d", height)
		assert.Lenf(t, txs, len(expectedTxs), "wrong len of txs at height=%d", height)
		// txs should be stored in order of txID
		utils.Sort(expectedTxs)

		txIDs := set.Set[ids.ID]{}
		for i := 0; i < len(txs); i++ {
			assert.Equalf(t, expectedTxs[i].ID().Hex(), txs[i].ID().Hex(), "wrong txID at height=%d idx=%d", height, i)
			txIDs.Add(txs[i].ID())
		}
		assert.Equalf(t, len(txs), txIDs.Len(), "incorrect number of unique transactions in slice at height %d, expected %d, found %d", height, len(txs), txIDs.Len())
	}
}

// verifyOperations creates an iterator over the atomicTrie at [rootHash] and verifies that the all of the operations in the trie in the interval [from, to] are identical to
// the atomic operations contained in [operationsMap] on the same interval.
func verifyOperations(t testing.TB, atomicTrie AtomicTrie, codec codec.Manager, rootHash common.Hash, from, to uint64, operationsMap map[uint64]map[ids.ID]*avalancheatomic.Requests) {
	t.Helper()

	// Start the iterator at [from]
	fromBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(fromBytes, from)
	iter, err := atomicTrie.Iterator(rootHash, fromBytes)
	if err != nil {
		t.Fatal(err)
	}

	// Generate map of the marshalled atomic operations on the interval [from, to]
	// based on [operationsMap].
	marshalledOperationsMap := make(map[uint64]map[ids.ID][]byte)
	for height, blockRequests := range operationsMap {
		if height < from || height > to {
			continue
		}
		for blockchainID, atomicRequests := range blockRequests {
			b, err := codec.Marshal(0, atomicRequests)
			if err != nil {
				t.Fatal(err)
			}
			if requestsMap, exists := marshalledOperationsMap[height]; exists {
				requestsMap[blockchainID] = b
			} else {
				requestsMap = make(map[ids.ID][]byte)
				requestsMap[blockchainID] = b
				marshalledOperationsMap[height] = requestsMap
			}
		}
	}

	// Generate map of marshalled atomic operations on the interval [from, to]
	// based on the contents of the trie.
	iteratorMarshalledOperationsMap := make(map[uint64]map[ids.ID][]byte)
	for iter.Next() {
		height := iter.BlockNumber()
		if height < from {
			t.Fatalf("Iterator starting at (%d) found value at block height (%d)", from, height)
		}
		if height > to {
			continue
		}

		blockchainID := iter.BlockchainID()
		b, err := codec.Marshal(0, iter.AtomicOps())
		if err != nil {
			t.Fatal(err)
		}
		if requestsMap, exists := iteratorMarshalledOperationsMap[height]; exists {
			requestsMap[blockchainID] = b
		} else {
			requestsMap = make(map[ids.ID][]byte)
			requestsMap[blockchainID] = b
			iteratorMarshalledOperationsMap[height] = requestsMap
		}
	}
	if err := iter.Error(); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, marshalledOperationsMap, iteratorMarshalledOperationsMap)
}

func TestAtomicRepositoryReadWriteSingleTx(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec
	repo, err := NewAtomicTxRepository(db, codec, 0)
	if err != nil {
		t.Fatal(err)
	}
	txMap := make(map[uint64][]*atomic.Tx)

	writeTxs(t, repo, 1, 100, constTxsPerHeight(1), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryReadWriteMultipleTxs(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec
	repo, err := NewAtomicTxRepository(db, codec, 0)
	if err != nil {
		t.Fatal(err)
	}
	txMap := make(map[uint64][]*atomic.Tx)

	writeTxs(t, repo, 1, 100, constTxsPerHeight(10), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryPreAP5Migration(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*atomic.Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 1, 100, 1, txMap, nil)
	if err := db.Commit(); err != nil {
		t.Fatal(err)
	}

	// Ensure the atomic repository can correctly migrate the transactions
	// from the old accepted atomic tx DB to add the height index.
	repo, err := NewAtomicTxRepository(db, codec, 100)
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)

	writeTxs(t, repo, 100, 150, constTxsPerHeight(1), txMap, nil)
	writeTxs(t, repo, 150, 200, constTxsPerHeight(10), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryPostAP5Migration(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*atomic.Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 1, 100, 1, txMap, nil)
	addTxs(t, codec, acceptedAtomicTxDB, 100, 200, 10, txMap, nil)
	if err := db.Commit(); err != nil {
		t.Fatal(err)
	}

	// Ensure the atomic repository can correctly migrate the transactions
	// from the old accepted atomic tx DB to add the height index.
	repo, err := NewAtomicTxRepository(db, codec, 200)
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)

	writeTxs(t, repo, 200, 300, constTxsPerHeight(10), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func benchAtomicRepositoryIndex10_000(b *testing.B, maxHeight uint64, txsPerHeight int) {
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*atomic.Tx)

	addTxs(b, codec, acceptedAtomicTxDB, 0, maxHeight, txsPerHeight, txMap, nil)
	if err := db.Commit(); err != nil {
		b.Fatal(err)
	}
	repo, err := NewAtomicTxRepository(db, codec, maxHeight)
	if err != nil {
		b.Fatal(err)
	}
	assert.NoError(b, err)
	verifyTxs(b, repo, txMap)
}

func BenchmarkAtomicRepositoryIndex_10kBlocks_1Tx(b *testing.B) {
	for n := 0; n < b.N; n++ {
		benchAtomicRepositoryIndex10_000(b, 10_000, 1)
	}
}

func BenchmarkAtomicRepositoryIndex_10kBlocks_10Tx(b *testing.B) {
	for n := 0; n < b.N; n++ {
		benchAtomicRepositoryIndex10_000(b, 10_000, 10)
	}
}
