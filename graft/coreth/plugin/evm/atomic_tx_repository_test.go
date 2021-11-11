package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
)

type TestTx struct {
	Id ids.ID `serialize:"true" json:"id"`
}

func (t TestTx) Initialize(unsignedBytes, signedBytes []byte) {
	// no op
}

func (t TestTx) ID() ids.ID {
	return t.Id
}

func (t TestTx) GasUsed() (uint64, error) {
	panic("implement me")
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

func (t TestTx) Verify(xChainID ids.ID, ctx *snow.Context, rules params.Rules) error {
	panic("implement me")
}

func (t TestTx) SemanticVerify(vm *VM, stx *Tx, parent *Block, baseFee *big.Int, rules params.Rules) error {
	panic("implement me")
}

func (t TestTx) AtomicOps() (map[ids.ID]*atomic.Requests, error) {
	panic("implement me")
}

func (t TestTx) Accept(ctx *snow.Context, batch database.Batch) error {
	panic("implement me")
}

func (t TestTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	panic("implement me")
}

func Test_AtomicTxRepository_Initialize(t *testing.T) {
	db := memdb.New()

	prepareCodecForTest()

	// write in the old database in legacy style
	txDB := prefixdb.New(atomicTxIDDBPrefix, db) // tx DB indexed by txID => height+txbytes
	txIDs := make([]ids.ID, 100)
	for i := 0; i < 100; i++ {
		id := ids.GenerateTestID()
		tx := &Tx{
			UnsignedAtomicTx: &TestTx{
				Id: id,
			},
		}

		txBytes, err := Codec.Marshal(codecVersion, tx)
		assert.NoError(t, err)
		assert.NotNil(t, txBytes)

		heightTxPacker := wrappers.Packer{Bytes: make([]byte, 12+len(txBytes))}
		heightTxPacker.PackLong(uint64(i))
		heightTxPacker.PackBytes(txBytes)

		err = txDB.Put(id[:], heightTxPacker.Bytes)
		assert.NoError(t, err)

		txIDs[i] = id
	}

	repo := newAtomicTxRepository(db, Codec)
	err := repo.Initialize()
	assert.NoError(t, err)

	for i := 0; i < 100; i++ {
		tx, err := repo.GetByHeight(uint64(i))
		assert.NoError(t, err)

		assert.Equal(t, txIDs[i], tx[0].ID())
	}
}

func prepareCodecForTest() {
	Codec = codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&UnsignedImportTx{}),
		c.RegisterType(&UnsignedExportTx{}),
	)
	c.SkipRegistrations(3)
	errs.Add(
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&secp256k1fx.Input{}),
		c.RegisterType(&secp256k1fx.OutputOwners{}),
		c.RegisterType(&TestTx{}),
		Codec.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

func Test_AtomicRepository_Read_Write(t *testing.T) {
	db := memdb.New()
	prepareCodecForTest()
	repo := newAtomicTxRepository(db, Codec)

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

	// check we can get them all by height
	for i := 0; i < 100; i++ {
		tx, err := repo.GetByHeight(uint64(i))
		assert.NoError(t, err)
		assert.Equal(t, tx[0].ID(), txIDs[i])
	}

	// check we can get them all by ID
	for i := 0; i < 100; i++ {
		tx, height, err := repo.GetByTxID(txIDs[i])
		assert.NoError(t, err)
		assert.EqualValues(t, height, i)
		assert.Equal(t, tx.ID(), txIDs[i])
	}
}
