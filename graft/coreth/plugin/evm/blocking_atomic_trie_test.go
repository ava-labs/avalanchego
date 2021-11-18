package evm

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/codec"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/stretchr/testify/assert"
)

type TestAtomicTx struct {
	avax.Metadata
	BlockchainID  ids.ID           `serialize:"true"`
	AtomicRequest *atomic.Requests `serialize:"true"`
}

func (t *TestAtomicTx) Bytes() []byte {
	b, err := rlp.EncodeToBytes(t)
	if err != nil {
		panic(err)
	}
	return b
}

func (t *TestAtomicTx) GasUsed() (uint64, error) {
	panic("implement me")
}

func (t *TestAtomicTx) Burned(assetID ids.ID) (uint64, error) {
	panic("implement me")
}

func (t *TestAtomicTx) InputUTXOs() ids.Set {
	panic("implement me")
}

func (t *TestAtomicTx) Verify(xChainID ids.ID, ctx *snow.Context, rules params.Rules) error {
	panic("implement me")
}

func (t *TestAtomicTx) SemanticVerify(vm *VM, stx *Tx, parent *Block, baseFee *big.Int, rules params.Rules) error {
	panic("implement me")
}

func (t *TestAtomicTx) AtomicOps() (map[ids.ID]*atomic.Requests, error) {
	return map[ids.ID]*atomic.Requests{t.BlockchainID: t.AtomicRequest}, nil
}

func (t *TestAtomicTx) Accept(ctx *snow.Context, batch database.Batch) error {
	panic("implement me")
}

func (t *TestAtomicTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	panic("implement me")
}

func testDataImportTx() *Tx {
	blockchainID := ids.GenerateTestID()
	return &Tx{UnsignedAtomicTx: &TestAtomicTx{
		BlockchainID: blockchainID,
		AtomicRequest: &atomic.Requests{
			PutRequests: []*atomic.Element{
				{
					Key:   utils.RandomBytes(16),
					Value: utils.RandomBytes(24),
					Traits: [][]byte{
						utils.RandomBytes(32),
						utils.RandomBytes(32),
					},
				},
			},
		},
	}}
}

func testDataExportTx() *Tx {
	blockchainID := ids.GenerateTestID()
	return &Tx{UnsignedAtomicTx: &TestAtomicTx{
		BlockchainID: blockchainID,
		AtomicRequest: &atomic.Requests{
			RemoveRequests: [][]byte{
				utils.RandomBytes(32),
				utils.RandomBytes(32),
			},
		}},
	}
}

func (t *Tx) mustAtomicOps() map[ids.ID]*atomic.Requests {
	ops, err := t.AtomicOps()
	if err != nil {
		panic(err)
	}
	return ops
}

func Test_BlockingAtomicTrie(t *testing.T) {
	db := memdb.New()
	Codec := codec.NewDefaultManager()

	c := linearcodec.NewDefault()
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&TestAtomicTx{}),
		Codec.RegisterCodec(codecVersion, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	repo := newAtomicTxRepository(db, Codec)
	txs := []*Tx{testDataExportTx(), testDataImportTx()}

	for height, tx := range txs {
		assert.NotNil(t, tx.Bytes())
		err := repo.Write(uint64(height), []*Tx{tx})
		assert.NoError(t, err)
	}

	atomicTrie, err := NewBlockingAtomicTrie(Database{db}, repo)
	assert.NoError(t, err)

	dbCommitFn := func() error {
		return nil
	}

	err = atomicTrie.Initialize(1, dbCommitFn)
	assert.NoError(t, err)
}

func Test_BlockingAtomicTrie_Initialize_Roots(t *testing.T) {
	db := memdb.New()
	Codec := codec.NewDefaultManager()

	c := linearcodec.NewDefault()
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&TestAtomicTx{}),
		Codec.RegisterCodec(codecVersion, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	repo := newAtomicTxRepository(db, Codec)

	const lastAcceptedHeight = 1000
	allTxs := make([][]*Tx, lastAcceptedHeight+1)
	for i := 0; i <= lastAcceptedHeight; i++ {
		txs := []*Tx{testDataImportTx()}
		if i%3 == 0 {
			txs = append(txs, testDataExportTx())
		}

		err := repo.Write(uint64(i), txs)
		assert.NoError(t, err)

		allTxs[i] = txs
	}

	atomicTrie, err := NewBlockingAtomicTrie(Database{db}, repo)
	assert.NoError(t, err)
	{
		blockingTrie := atomicTrie.(*blockingAtomicTrie)
		blockingTrie.commitHeightInterval = 10 // commit every 10 blocks for test
	}

	dbCommitFn := func() error {
		return nil
	}

	err = atomicTrie.Initialize(lastAcceptedHeight, dbCommitFn)
	assert.NoError(t, err)
}

func Test_BlockingAtomicTrie_XYZ(t *testing.T) {
	db := memdb.New()
	vdb := versiondb.New(db)
	repo := newAtomicTxRepository(db, Codec)
	atomicTrie, err := NewBlockingAtomicTrie(Database{vdb}, repo)
	assert.NoError(t, err)
	dbCommitFn := func() error {
		return nil
	}

	err = atomicTrie.Initialize(100, dbCommitFn)
	assert.NoError(t, err)

	tx0 := testDataExportTx()
	_, err = atomicTrie.Index(0, tx0.mustAtomicOps())
	assert.NoError(t, err)

	tx1 := testDataImportTx()
	_, err = atomicTrie.Index(1, tx1.mustAtomicOps())
	assert.NoError(t, err)

	vdb.Commit()

	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, 0)
	val, err := vdb.Get(key)
	assert.NoError(t, err)
	fmt.Println("val", len(val))
}
