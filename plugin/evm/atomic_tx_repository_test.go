// (c) 2020-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.
package evm

import (
	"sort"
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

// addTxs writes [txPerHeight] txs for heights ranging in [fromHeight] to [toHeight] directly to [acceptedAtomicTxDB],
// storing the results in [txMap] for verifying by verifyTxs
func addTxs(t *testing.T, codec codec.Manager, acceptedAtomicTxDB database.Database, fromHeight uint64, toHeight uint64, txPerHeight int, txMap map[uint64][]*Tx) {
	for height := fromHeight; height < toHeight; height++ {
		for i := 0; i < txPerHeight; i++ {
			id, tx := newTestTx()
			txBytes, err := codec.Marshal(codecVersion, tx)
			assert.NoError(t, err)

			// Write atomic transactions to the [acceptedAtomicTxDB]
			// in the format handled prior to the migration to the atomic
			// tx repository.
			packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
			packer.PackLong(height)
			packer.PackBytes(txBytes)
			err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
			assert.NoError(t, err)

			// save this to the map for verifying expected results in verifyTxs
			txMap[height] = append(txMap[height], tx)
		}
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

func TestAtomicRepositoryReadWrite(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()
	repo := NewAtomicTxRepository(db, codec)
	txMap := make(map[uint64][]*Tx)

	writeTxs(repo, 0, 100, 1, txMap)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryInitialize(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 0, 100, 1, txMap)

	repo := NewAtomicTxRepository(db, codec)
	err := repo.Initialize(nil)
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)

	// add some more Txs
	addTxs(t, codec, acceptedAtomicTxDB, 100, 150, 1, txMap)
	repo = NewAtomicTxRepository(db, codec)
	err = repo.Initialize(nil)
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryInitializeMultipleHeights(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)

	addTxs(t, codec, acceptedAtomicTxDB, 0, 50, 1, txMap)
	addTxs(t, codec, acceptedAtomicTxDB, 50, 100, 2, txMap) // multiple txs per height

	repo := NewAtomicTxRepository(db, codec)
	err := repo.Initialize(nil)
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)

	// add some more Tx
	addTxs(t, codec, acceptedAtomicTxDB, 100, 150, 1, txMap)

	repo = NewAtomicTxRepository(db, codec)
	err = repo.Initialize(nil)
	assert.NoError(t, err)
	verifyTxs(t, repo, txMap)
}
