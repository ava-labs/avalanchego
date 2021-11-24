package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database/prefixdb"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

type TestTx struct {
	Id ids.ID `serialize:"true" json:"id"`
}

func (t TestTx) GasUsed(fixedFee bool) (uint64, error) {
	panic("implement me")
}

func (t TestTx) Verify(ctx *snow.Context, rules params.Rules) error {
	panic("implement me")
}

func (t TestTx) Accept() (ids.ID, *atomic.Requests, error) {
	panic("implement me")
}

func (t TestTx) Initialize(unsignedBytes, signedBytes []byte) {
	// no op
}

func (t TestTx) ID() ids.ID {
	return t.Id
}

func (t TestTx) Burned(assetID ids.ID) (uint64, error) {
	panic("implement me")
}

func (t TestTx) UnsignedBytes() []byte {
	panic("implement me")
}

func (t TestTx) Bytes() []byte {
	panic("implement me")
}

func (t TestTx) InputUTXOs() ids.Set {
	panic("implement me")
}

func (t TestTx) SemanticVerify(vm *VM, stx *Tx, parent *Block, baseFee *big.Int, rules params.Rules) error {
	panic("implement me")
}

func (t TestTx) AtomicOps() (map[ids.ID]*atomic.Requests, error) {
	panic("implement me")
}

func (t TestTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	panic("implement me")
}

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

func TestAtomicRepositoryReadWrite(t *testing.T) {
	db := memdb.New()
	codec := prepareCodecForTest()
	repo := newAtomicTxRepository(db, codec)

	txIDs := make([]ids.ID, 100)
	for i := 0; i < 100; i++ {
		id := ids.GenerateTestID()
		tx := &Tx{
			UnsignedAtomicTx: &TestTx{
				Id: id,
			},
		}

		err := repo.Write(uint64(i), []*Tx{tx})
		assert.NoError(t, err)

		txIDs[i] = id
	}

	// check we can get them all by ID
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

	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	txIDs := make([]ids.ID, 150)
	for i := 0; i < 100; i++ {
		id := ids.GenerateTestID()
		tx := &Tx{
			UnsignedAtomicTx: &TestTx{
				Id: id,
			},
		}

		txBytes, err := codec.Marshal(codecVersion, tx)
		assert.NoError(t, err)
		packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
		packer.PackLong(uint64(i))
		packer.PackBytes(txBytes)
		err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
		assert.NoError(t, err)
		txIDs[i] = id
	}

	repo := newAtomicTxRepository(db, codec)
	err := repo.Initialize()
	assert.NoError(t, err)

	// check we can get them all by ID
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
		id := ids.GenerateTestID()
		tx := &Tx{
			UnsignedAtomicTx: &TestTx{
				Id: id,
			},
		}

		txBytes, err := codec.Marshal(codecVersion, tx)
		assert.NoError(t, err)
		packer := wrappers.Packer{Bytes: make([]byte, 1), MaxSize: 1024 * 1024}
		packer.PackLong(uint64(i))
		packer.PackBytes(txBytes)
		err = acceptedAtomicTxDB.Put(id[:], packer.Bytes)
		assert.NoError(t, err)
		txIDs[i] = id
	}

	repo = newAtomicTxRepository(db, codec)
	err = repo.Initialize()
	assert.NoError(t, err)

	// check we can get them all by ID
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
