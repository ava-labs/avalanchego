// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

// addTxs writes [txsPerHeight] txs for heights ranging in [fromHeight, toHeight) directly to [acceptedAtomicTxDB],
// storing the resulting transactions in [txMap] if non-nil and the resulting atomic operations in [operationsMap]
// if non-nil.
func addTxs(t testing.TB, codec codec.Manager, acceptedAtomicTxDB database.Database, fromHeight uint64, toHeight uint64, txsPerHeight int, txMap map[uint64][]*atomic.Tx) {
	for height := fromHeight; height < toHeight; height++ {
		txs := make([]*atomic.Tx, 0, txsPerHeight)
		for i := 0; i < txsPerHeight; i++ {
			tx := atomictest.NewTestTx()
			txs = append(txs, tx)
			txBytes, err := codec.Marshal(atomic.CodecVersion, tx)
			require.NoError(t, err)

			// Write atomic transactions to the [acceptedAtomicTxDB]
			// in the format handled prior to the migration to the atomic
			// tx repository.
			packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
			packer.PackLong(height)
			packer.PackBytes(txBytes)
			txID := tx.ID()
			require.NoError(t, acceptedAtomicTxDB.Put(txID[:], packer.Bytes))
		}
		// save this to the map (if non-nil) for verifying expected results in verifyTxs
		if txMap != nil {
			txMap[height] = txs
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
func writeTxs(t testing.TB, repo *AtomicRepository, fromHeight uint64, toHeight uint64,
	txsPerHeight func(height uint64) int, txMap map[uint64][]*atomic.Tx, operationsMap map[uint64]map[ids.ID]*avalancheatomic.Requests,
) {
	for height := fromHeight; height < toHeight; height++ {
		txs := atomictest.NewTestTxs(txsPerHeight(height))
		require.NoError(t, repo.Write(height, txs))
		// save this to the map (if non-nil) for verifying expected results in verifyTxs
		if txMap != nil {
			txMap[height] = txs
		}
		if operationsMap != nil {
			atomicRequests, err := mergeAtomicOps(txs)
			require.NoError(t, err)
			if len(atomicRequests) == 0 {
				continue
			}
			operationsMap[height] = atomicRequests
		}
	}
}

// verifyTxs requires [repo] can find all txs in [txMap] by height and txID
func verifyTxs(t testing.TB, repo *AtomicRepository, txMap map[uint64][]*atomic.Tx) {
	// We should be able to fetch indexed txs by height:
	for height, expectedTxs := range txMap {
		txs, err := repo.GetByHeight(height)
		require.NoErrorf(t, err, "unexpected error on GetByHeight at height=%d", height)
		require.Lenf(t, txs, len(expectedTxs), "wrong len of txs at height=%d", height)
		// txs should be stored in order of txID
		utils.Sort(expectedTxs)

		txIDs := set.Set[ids.ID]{}
		for i := 0; i < len(txs); i++ {
			require.Equalf(t, expectedTxs[i].ID().Hex(), txs[i].ID().Hex(), "wrong txID at height=%d idx=%d", height, i)
			txIDs.Add(txs[i].ID())
		}
		require.Equalf(t, len(txs), txIDs.Len(), "incorrect number of unique transactions in slice at height %d, expected %d, found %d", height, len(txs), txIDs.Len())
	}
}

func TestAtomicRepositoryReadWriteSingleTx(t *testing.T) {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, 0)
	require.NoError(t, err)
	txMap := make(map[uint64][]*atomic.Tx)

	writeTxs(t, repo, 1, 100, constTxsPerHeight(1), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryReadWriteMultipleTxs(t *testing.T) {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, 0)
	require.NoError(t, err)
	txMap := make(map[uint64][]*atomic.Tx)

	writeTxs(t, repo, 1, 100, constTxsPerHeight(10), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryPreAP5Migration(t *testing.T) {
	db := versiondb.New(memdb.New())

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*atomic.Tx)
	addTxs(t, atomictest.TestTxCodec, acceptedAtomicTxDB, 1, 100, 1, txMap)
	require.NoError(t, db.Commit())

	// Ensure the atomic repository can correctly migrate the transactions
	// from the old accepted atomic tx DB to add the height index.
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, 100)
	require.NoError(t, err)
	verifyTxs(t, repo, txMap)

	writeTxs(t, repo, 100, 150, constTxsPerHeight(1), txMap, nil)
	writeTxs(t, repo, 150, 200, constTxsPerHeight(10), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryPostAP5Migration(t *testing.T) {
	db := versiondb.New(memdb.New())

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*atomic.Tx)
	addTxs(t, atomictest.TestTxCodec, acceptedAtomicTxDB, 1, 100, 1, txMap)
	addTxs(t, atomictest.TestTxCodec, acceptedAtomicTxDB, 100, 200, 10, txMap)
	require.NoError(t, db.Commit())

	// Ensure the atomic repository can correctly migrate the transactions
	// from the old accepted atomic tx DB to add the height index.
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, 200)
	require.NoError(t, err)
	verifyTxs(t, repo, txMap)

	writeTxs(t, repo, 200, 300, constTxsPerHeight(10), txMap, nil)
	verifyTxs(t, repo, txMap)
}

func benchAtomicRepositoryIndex10_000(b *testing.B, maxHeight uint64, txsPerHeight int) {
	db := versiondb.New(memdb.New())

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*atomic.Tx)

	addTxs(b, atomictest.TestTxCodec, acceptedAtomicTxDB, 0, maxHeight, txsPerHeight, txMap)
	require.NoError(b, db.Commit())
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, maxHeight)
	require.NoError(b, err)
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
