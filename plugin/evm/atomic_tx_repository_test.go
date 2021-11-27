// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.
package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

func prepareCodecForTest() codec.Manager {
	codec := codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&TestTx{}),
		codec.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
	return codec
}

func newTestTx() (ids.ID, *Tx) {
	id := ids.GenerateTestID()
	return id, &Tx{UnsignedAtomicTx: &TestTx{IDV: id}}
}

// Tests simple writing and reading behaviour from atomic repository
func TestAtomicRepositoryReadWrite(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()
	repo := NewAtomicTxRepository(db, codec)

	// Generate and write atomic transactions to the repository
	txIDs := make([]ids.ID, 100)
	for i := 0; i < 100; i++ {
		id, tx := newTestTx()

		err := repo.Write(uint64(i), []*Tx{tx})
		assert.NoError(t, err)

		txIDs[i] = id
	}
}

// writeTxs writes [txPerHeight] txs for heights ranging in [fromHeight] to [toHeight] through the Write call on [repo],
// storing the results in [txMap] for verifying by verifyTxs
func writeTxs(repo AtomicTxRepository, fromHeight uint64, toHeight uint64, txPerHeight int, txMap map[uint64][]*Tx) {
	for height := fromHeight; height < toHeight; height++ {
		txs := make([]*Tx, 0)
		for i := 0; i < txPerHeight; i++ {
			_, tx := newTestTx()
			txs = append(txs, tx)
		}
		repo.Write(height, txs)
		// save this to the map for verifying expected results in verifyTxs
		txMap[height] = txs
	}
}

// verifyTxs asserts [repo] can find all txs in [txMap] by height and txID
func verifyTxs(t *testing.T, repo AtomicTxRepository, txMap map[uint64][]*Tx) {
	// We should be able to fetch indexed txs by height:
	for height, expectedTxs := range txMap {
		txs, err := repo.GetByHeight(height)
		assert.NoErrorf(t, err, "expected err=nil on GetByHeight at height=%d", height)
		assert.Lenf(t, txs, len(expectedTxs), "wrong len of txs at height=%d", height)
		// txs should be stored in order of txID
		sort.Slice(expectedTxs, func(i, j int) bool {
			return expectedTxs[i].ID().Hex() < expectedTxs[j].ID().Hex()
		})
		for i := 0; i < len(txs); i++ {
			assert.Equalf(t, expectedTxs[i].ID().Hex(), txs[i].ID().Hex(), "wrong txID at height=%d idx=%d", height, i)
		}
	}
}

// Tests simple Initialize behaviour from the atomic repository
// based on a pre-populated txID=height+txbytes entries in the
// acceptedAtomicTxDB database
func TestAtomicRepositoryInitialize(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 0, 100, 1, txMap)

	repo := NewAtomicTxRepository(db, codec)
	err := repo.Initialize(50)
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)

	// add some more Txs
	addTxs(t, codec, acceptedAtomicTxDB, 100, 150, 1, txMap)
	repo = NewAtomicTxRepository(db, codec)
	err = repo.Initialize(0)
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)
}

// Test ensures Initialize can handle multiple atomic transactions past a
// given block
func TestAtomicRepositoryInitializeHandlesMultipleAtomicTxs(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	heightTxIDMap := make(map[uint64][]ids.ID, 175)

	const apricotPhase5Height = uint64(50)

	for i := uint64(0); i < 150; i++ {
		txs := make(map[ids.ID]*Tx)
		id, tx := newTestTx()
		txs[id] = tx

		// enable multiple txs for every other block past block 50
		if i > apricotPhase5Height && i%2 == 0 {
			id, tx := newTestTx()
			txs[id] = tx
		}

		txList := make([]ids.ID, 0, len(txs))
		for id, tx := range txs {
			txBytes, err := codec.Marshal(codecVersion, tx)
			assert.NoError(t, err)

			packer := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes))}
			packer.PackLong(i)
			packer.PackBytes(txBytes)
			err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
			assert.NoError(t, err)

			txList = append(txList, id)
		}

		heightTxIDMap[i] = txList
	}

	assert.Len(t, heightTxIDMap, 150)

	repo := NewAtomicTxRepository(db, codec)
	err := repo.Initialize(apricotPhase5Height)
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)

	for height, txIDs := range heightTxIDMap {
		// first assert the height index
		txs, err := repo.GetByHeight(height)
		assert.NoError(t, err)
		assert.Len(t, txs, len(txIDs))

		txIDSet := make(map[ids.ID]struct{}, len(txIDs))
		for _, txID := range txIDs {
			txIDSet[txID] = struct{}{}
		}

		for _, tx := range txs {
			_, exists := txIDSet[tx.ID()]
			assert.True(t, exists)
		}

		// now assert the txID index
		for _, txID := range txIDs {
			tx, txHeight, err := repo.GetByTxID(txID)
			assert.NoError(t, err)
			assert.Equal(t, height, txHeight)
			assert.Equal(t, txID, tx.ID())
		}
	}
}

// Test ensures Initialize can handle multiple atomic transactions past a
// given block
func TestAtomicRepositoryInitializeHandlesMultipleAtomicTxs_Bench(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	heightTxIDMap := make(map[uint64][]ids.ID, 175)

	const apricotPhase5Height = uint64(700000)

	for i := uint64(0); i < 1000000; i++ {
		txs := make(map[ids.ID]*Tx)
		id, tx := newTestTx()
		txs[id] = tx

		// enable multiple txs for every other block past block 50
		if i > apricotPhase5Height && i%2 == 0 {
			id, tx := newTestTx()
			txs[id] = tx
		}

		txList := make([]ids.ID, 0, len(txs))
		for id, tx := range txs {
			txBytes, err := codec.Marshal(codecVersion, tx)
			assert.NoError(t, err)

			packer := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes))}
			packer.PackLong(i)
			packer.PackBytes(txBytes)
			err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
			assert.NoError(t, err)

			txList = append(txList, id)
		}

		heightTxIDMap[i] = txList
	}

	assert.Len(t, heightTxIDMap, 1000000)

	repo := NewAtomicTxRepository(db, codec)
	err := repo.Initialize(apricotPhase5Height)
	assert.NoError(t, err)

	for height, txIDs := range heightTxIDMap {
		// first assert the height index
		txs, err := repo.GetByHeight(height)
		assert.NoError(t, err)
		assert.Len(t, txs, len(txIDs))

		txIDSet := make(map[ids.ID]struct{}, len(txIDs))
		for _, txID := range txIDs {
			txIDSet[txID] = struct{}{}
		}

		for _, tx := range txs {
			_, exists := txIDSet[tx.ID()]
			assert.True(t, exists)
		}

		// now assert the txID index
		for _, txID := range txIDs {
			tx, txHeight, err := repo.GetByTxID(txID)
			assert.NoError(t, err)
			assert.Equal(t, height, txHeight)
			assert.Equal(t, txID, tx.ID())
		}
	}
}
