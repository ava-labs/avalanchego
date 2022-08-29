// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"fmt"
	"sort"
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

// addTxs writes [txsPerHeight] txs for heights ranging in [fromHeight, toHeight) directly to [acceptedAtomicTxDB],
// storing the resulting transactions in [txMap] if non-nil and the resulting atomic operations in [operationsMap]
// if non-nil.
func addTxs(t testing.TB, codec codec.Manager, acceptedAtomicTxDB database.Database, fromHeight uint64, toHeight uint64, txsPerHeight int, txMap map[uint64][]*Tx, operationsMap map[uint64]map[ids.ID]*atomic.Requests) {
	for height := fromHeight; height < toHeight; height++ {
		txs := make([]*Tx, 0, txsPerHeight)
		for i := 0; i < txsPerHeight; i++ {
			tx := newTestTx()
			txs = append(txs, tx)
			txBytes, err := codec.Marshal(codecVersion, tx)
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
	txsPerHeight func(height uint64) int, txMap map[uint64][]*Tx, operationsMap map[uint64]map[ids.ID]*atomic.Requests,
) {
	for height := fromHeight; height < toHeight; height++ {
		txs := newTestTxs(txsPerHeight(height))
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
func verifyTxs(t testing.TB, repo AtomicTxRepository, txMap map[uint64][]*Tx) {
	// We should be able to fetch indexed txs by height:
	getComparator := func(txs []*Tx) func(int, int) bool {
		return func(i, j int) bool {
			return txs[i].ID().Hex() < txs[j].ID().Hex()
		}
	}
	for height, expectedTxs := range txMap {
		txs, err := repo.GetByHeight(height)
		assert.NoErrorf(t, err, "unexpected error on GetByHeight at height=%d", height)
		assert.Lenf(t, txs, len(expectedTxs), "wrong len of txs at height=%d", height)
		// txs should be stored in order of txID
		sort.Slice(expectedTxs, getComparator(expectedTxs))

		txIDs := ids.Set{}
		for i := 0; i < len(txs); i++ {
			assert.Equalf(t, expectedTxs[i].ID().Hex(), txs[i].ID().Hex(), "wrong txID at height=%d idx=%d", height, i)
			txIDs.Add(txs[i].ID())
		}
		assert.Equalf(t, len(txs), txIDs.Len(), "incorrect number of unique transactions in slice at height %d, expected %d, found %d", height, len(txs), txIDs.Len())
	}
}

// verifyOperations creates an iterator over the atomicTrie at [rootHash] and verifies that the all of the operations in the trie in the interval [from, to] are identical to
// the atomic operations contained in [operationsMap] on the same interval.
func verifyOperations(t testing.TB, atomicTrie AtomicTrie, codec codec.Manager, rootHash common.Hash, from, to uint64, operationsMap map[uint64]map[ids.ID]*atomic.Requests) {
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
	codec := testTxCodec()
	repo, err := NewAtomicTxRepository(db, codec, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	txMap := make(map[uint64][]*Tx)

	writeTxs(t, repo, 1, 100, constTxsPerHeight(1), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryReadWriteMultipleTxs(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := testTxCodec()
	repo, err := NewAtomicTxRepository(db, codec, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	txMap := make(map[uint64][]*Tx)

	writeTxs(t, repo, 1, 100, constTxsPerHeight(10), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryPreAP5Migration(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := testTxCodec()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 1, 100, 1, txMap, nil)
	if err := db.Commit(); err != nil {
		t.Fatal(err)
	}

	// Ensure the atomic repository can correctly migrate the transactions
	// from the old accepted atomic tx DB to add the height index.
	repo, err := NewAtomicTxRepository(db, codec, 100, nil, nil, nil)
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
	codec := testTxCodec()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 1, 100, 1, txMap, nil)
	addTxs(t, codec, acceptedAtomicTxDB, 100, 200, 10, txMap, nil)
	if err := db.Commit(); err != nil {
		t.Fatal(err)
	}

	// Ensure the atomic repository can correctly migrate the transactions
	// from the old accepted atomic tx DB to add the height index.
	repo, err := NewAtomicTxRepository(db, codec, 200, nil, nil, nil)
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
	codec := testTxCodec()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)

	addTxs(b, codec, acceptedAtomicTxDB, 0, maxHeight, txsPerHeight, txMap, nil)
	if err := db.Commit(); err != nil {
		b.Fatal(err)
	}
	repo, err := NewAtomicTxRepository(db, codec, maxHeight, nil, nil, nil)
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

func TestRepairAtomicRepositoryForBonusBlockTxs(t *testing.T) {
	db := versiondb.New(memdb.New())
	atomicTxRepository, err := NewAtomicTxRepository(db, testTxCodec(), 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// check completion flag is set
	done, err := atomicTxRepository.isBonusBlocksRepaired()
	assert.NoError(t, err)
	assert.True(t, done)

	// delete the key so we can simulate an unrepaired repository
	atomicTxRepository.atomicRepoMetadataDB.Delete(bonusBlocksRepairedKey)

	tx := newTestTx()
	// write the same tx to 3 heights.
	canonical, bonus1, bonus2 := uint64(10), uint64(20), uint64(30)
	atomicTxRepository.Write(canonical, []*Tx{tx})
	atomicTxRepository.Write(bonus1, []*Tx{tx})
	atomicTxRepository.Write(bonus2, []*Tx{tx})
	db.Commit()

	_, foundHeight, err := atomicTxRepository.GetByTxID(tx.ID())
	assert.NoError(t, err)
	assert.Equal(t, bonus2, foundHeight)

	allHeights := []uint64{canonical, bonus1, bonus2}
	if err := atomicTxRepository.RepairForBonusBlocks(
		allHeights,
		func(height uint64) (*Tx, error) {
			if height == 10 || height == 20 || height == 30 {
				return tx, nil
			}
			return nil, fmt.Errorf("unexpected height %d", height)
		},
	); err != nil {
		t.Fatal(err)
	}

	// check canonical height is indexed against txID
	_, foundHeight, err = atomicTxRepository.GetByTxID(tx.ID())
	assert.NoError(t, err)
	assert.Equal(t, canonical, foundHeight)

	// check tx can be found with any of the heights
	for _, height := range allHeights {
		txs, err := atomicTxRepository.GetByHeight(height)
		if err != nil {
			t.Fatal(err)
		}
		assert.Len(t, txs, 1)
		assert.Equal(t, tx.ID(), txs[0].ID())
	}

	// check completion flag is set
	done, err = atomicTxRepository.isBonusBlocksRepaired()
	assert.NoError(t, err)
	assert.True(t, done)
}
