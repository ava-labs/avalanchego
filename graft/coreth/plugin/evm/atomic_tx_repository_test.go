package evm

import (
	"bytes"
	"testing"

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

	// Verify that we can fetch all of the indexed transactions
	// by their txID and height.
	for i := 0; i < 100; i++ {
		tx, height, err := repo.GetByTxID(txIDs[i])
		assert.NoError(t, err)
		assert.EqualValues(t, height, i)
		assert.Equal(t, tx.ID(), txIDs[i])

		txs, err := repo.GetByHeight(height)
		assert.NoError(t, err)
		assert.Len(t, txs, 1)
		assert.Equal(t, txIDs[i], txs[0].ID())
	}
}

func TestAtomicRepositoryInitialize(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()

	// Write atomic transactions to the [acceptedAtomicTxDB]
	// in the format handled prior to the migration to the atomic
	// tx repository.
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txIDs := make([]ids.ID, 150)
	for i := 0; i < 100; i++ {
		id, tx := newTestTx()

		txBytes, err := codec.Marshal(codecVersion, tx)
		assert.NoError(t, err)

		packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
		packer.PackLong(uint64(i))
		packer.PackBytes(txBytes)
		err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
		assert.NoError(t, err)
		txIDs[i] = id
	}

	repo := NewAtomicTxRepository(db, codec)
	err := repo.Initialize()
	assert.NoError(t, err)

	// Verify that we can fetch all of the indexed transactions by their txID and height.
	for i := 0; i < 100; i++ {
		tx, height, err := repo.GetByTxID(txIDs[i])
		assert.NoError(t, err)
		assert.EqualValues(t, height, i)
		assert.Equal(t, tx.ID(), txIDs[i])

		txs, err := repo.GetByHeight(height)
		assert.NoError(t, err)
		assert.Len(t, txs, 1)
		assert.Equal(t, txIDs[i], txs[0].ID())
	}

	for i := 100; i < 150; i++ {
		id, tx := newTestTx()

		txBytes, err := codec.Marshal(codecVersion, tx)
		assert.NoError(t, err)
		packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
		packer.PackLong(uint64(i))
		packer.PackBytes(txBytes)
		err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
		assert.NoError(t, err)
		txIDs[i] = id
	}

	repo = NewAtomicTxRepository(db, codec)
	err = repo.Initialize()
	assert.NoError(t, err)

	// Verify that we can fetch all of the indexed transactions by their txID and height.
	for i := 0; i < 150; i++ {
		tx, height, err := repo.GetByTxID(txIDs[i])
		assert.NoError(t, err)
		assert.EqualValues(t, height, i)
		assert.Equal(t, tx.ID(), txIDs[i])

		txs, err := repo.GetByHeight(height)
		assert.NoError(t, err, "error '%v' for height %d", err, height)
		assert.Len(t, txs, 1)
		assert.Equal(t, txIDs[i], txs[0].ID())
	}
}

func TestAtomicRepositoryInitializeMultipleHeights(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txIDs := make([]ids.ID, 150)

	getHeight := func(idx int) uint64 {
		if idx >= 50 && idx < 100 && idx%2 == 1 {
			return uint64(idx) - 1
		}
		return uint64(idx)
	}
	for i := 0; i < 100; i++ {
		id, tx := newTestTx()

		txBytes, err := Codec.Marshal(codecVersion, tx)
		assert.NoError(t, err)

		packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
		packer.PackLong(getHeight(i))
		packer.PackBytes(txBytes)
		err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
		assert.NoError(t, err)
		txIDs[i] = id

	}

	repo := NewAtomicTxRepository(db, codec)
	err := repo.Initialize()
	assert.NoError(t, err)

	// Verify that we can fetch all of the indexed transactions by their txID and height.
	for i := 0; i < 100; i++ {
		height := getHeight(i)
		tx, txHeight, err := repo.GetByTxID(txIDs[i])
		assert.NoError(t, err)
		assert.EqualValues(t, height, txHeight)
		assert.Equal(t, tx.ID(), txIDs[i])

		txs, err := repo.GetByHeight(height)
		assert.NoError(t, err)
		if i < 50 {
			assert.Len(t, txs, 1)
			assert.Equal(t, txIDs[i], txs[0].ID())
		} else {
			assert.Len(t, txs, 2)
			resultIDs := []ids.ID{txs[0].ID(), txs[1].ID()}
			assert.Contains(t, resultIDs, txIDs[i], "expecting txs to contain txIDs[i]")
			assert.Negative(t, bytes.Compare(resultIDs[0][:], resultIDs[1][:]), "expecting txs to be sorted on txID")
		}
	}

	for i := 100; i < 150; i++ {
		id, tx := newTestTx()

		txBytes, err := Codec.Marshal(codecVersion, tx)
		assert.NoError(t, err)
		packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
		packer.PackLong(uint64(i))
		packer.PackBytes(txBytes)
		err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
		assert.NoError(t, err)
		txIDs[i] = id
	}

	repo = NewAtomicTxRepository(db, codec)
	err = repo.Initialize()
	assert.NoError(t, err)

	// Verify that we can fetch all of the indexed transactions by their txID.
	for i := 0; i < 150; i++ {
		height := getHeight(i)
		tx, txHeight, err := repo.GetByTxID(txIDs[i])
		assert.NoError(t, err)
		assert.EqualValues(t, height, txHeight)
		assert.Equal(t, tx.ID(), txIDs[i])
	}

	// Verify that we can fetch all of the indexed transactions by their height.
	for i := 100; i < 150; i++ {
		height := getHeight(i)
		txs, err := repo.GetByHeight(height)
		assert.NoError(t, err, "error '%v' for height %d", err, height)
		assert.Len(t, txs, 1)
		assert.Equal(t, txIDs[i], txs[0].ID())
	}
}
