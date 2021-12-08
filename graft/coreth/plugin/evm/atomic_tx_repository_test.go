// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package evm

import (
	"sort"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"

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

// addTxs writes [txsPerHeight] txs for heights ranging in [fromHeight, toHeight) directly to [acceptedAtomicTxDB],
// storing the results in [txMap] for verifying by verifyTxs
func addTxs(t testing.TB, codec codec.Manager, acceptedAtomicTxDB database.Database, fromHeight uint64, toHeight uint64, txsPerHeight int, txMap map[uint64][]*Tx) {
	for height := fromHeight; height < toHeight; height++ {
		for i := 0; i < txsPerHeight; i++ {
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

// writeTxs writes [txsPerHeight] txs for heights ranging in [fromHeight, toHeight) through the Write call on [repo],
// storing the results in [txMap] for verifying by verifyTxs
func writeTxs(t testing.TB, repo AtomicTxRepository, fromHeight uint64, toHeight uint64, txsPerHeight int, txMap map[uint64][]*Tx) {
	for height := fromHeight; height < toHeight; height++ {
		txs := make([]*Tx, 0)
		for i := 0; i < txsPerHeight; i++ {
			_, tx := newTestTx()
			txs = append(txs, tx)
		}
		if err := repo.Write(height, txs); err != nil {
			t.Fatal(err)
		}
		// save this to the map for verifying expected results in verifyTxs
		txMap[height] = txs
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

func TestAtomicRepositoryReadWriteSingleTx(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := prepareCodecForTest()
	repo, err := NewAtomicTxRepository(db, codec, 0)
	if err != nil {
		t.Fatal(err)
	}
	txMap := make(map[uint64][]*Tx)

	writeTxs(t, repo, 0, 100, 1, txMap)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryReadWriteMultipleTxs(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := prepareCodecForTest()
	repo, err := NewAtomicTxRepository(db, codec, 0)
	if err != nil {
		t.Fatal(err)
	}
	txMap := make(map[uint64][]*Tx)

	writeTxs(t, repo, 0, 100, 10, txMap)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryPreAP5Migration(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 0, 100, 1, txMap)
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

	writeTxs(t, repo, 100, 150, 1, txMap)
	writeTxs(t, repo, 150, 200, 10, txMap)
	verifyTxs(t, repo, txMap)
}

func TestAtomicRepositoryPostAP5Migration(t *testing.T) {
	db := versiondb.New(memdb.New())
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)
	addTxs(t, codec, acceptedAtomicTxDB, 0, 100, 1, txMap)
	addTxs(t, codec, acceptedAtomicTxDB, 100, 200, 10, txMap)
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

	writeTxs(t, repo, 200, 300, 10, txMap)
	verifyTxs(t, repo, txMap)
}

func benchAtomicRepositoryIndex10_000(b *testing.B, maxHeight uint64, txsPerHeight int) {
	db := versiondb.New(memdb.New())
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txMap := make(map[uint64][]*Tx)

	addTxs(b, codec, acceptedAtomicTxDB, 0, maxHeight, txsPerHeight, txMap)
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
