package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/ethdb/memorydb"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/codec"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
	"github.com/stretchr/testify/assert"
)

type TestAtomicTx struct {
	avax.Metadata
	TxID          ids.ID           `serialize:"true"`
	BlockchainID  ids.ID           `serialize:"true"`
	AtomicRequest *atomic.Requests `serialize:"true"`
}

func (t *TestAtomicTx) ID() ids.ID {
	return t.TxID
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
		TxID:         ids.GenerateTestID(),
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

func testCodec() codec.Manager {
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
	return Codec
}

func Test_BlockingAtomicTrie_InitializeGenesis(t *testing.T) {
	db := memdb.New()
	repo := newAtomicTxRepository(db, testCodec())
	tx := testDataImportTx()
	err := repo.Write(0, []*Tx{tx})
	assert.NoError(t, err)

	atomicTrieDB := memorydb.New()
	atomicTrie, err := NewBlockingAtomicTrie(atomicTrieDB, repo)
	assert.NoError(t, err)
	atomicTrie.(*blockingAtomicTrie).commitHeightInterval = 10
	dbCommitFn := func() error {
		return nil
	}
	err = atomicTrie.Initialize(0, dbCommitFn)
	assert.NoError(t, err)

	hash, num, err := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, hash)
	assert.EqualValues(t, 0, num)
}

func Test_BlockingAtomicTrie_InitializeGenesisPlusOne(t *testing.T) {
	db := memdb.New()
	repo := newAtomicTxRepository(db, testCodec())
	err := repo.Write(0, []*Tx{testDataImportTx()})
	assert.NoError(t, err)
	err = repo.Write(1, []*Tx{testDataImportTx()})
	assert.NoError(t, err)

	atomicTrieDB := memorydb.New()
	atomicTrie, err := NewBlockingAtomicTrie(atomicTrieDB, repo)
	assert.NoError(t, err)
	atomicTrie.(*blockingAtomicTrie).commitHeightInterval = 10
	dbCommitFn := func() error {
		return nil
	}
	err = atomicTrie.Initialize(1, dbCommitFn)
	assert.NoError(t, err)

	hash, num, err := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, hash)
	assert.EqualValues(t, 0, num, "expected %d was %d", 0, num)
}

func Test_BlockingAtomicTrie_Initialize(t *testing.T) {
	db := memdb.New()
	repo := newAtomicTxRepository(db, testCodec())
	// create state
	lastAcceptedHeight := uint64(1000)
	for i := uint64(0); i <= lastAcceptedHeight; i++ {
		err := repo.Write(i, []*Tx{testDataExportTx()})
		assert.NoError(t, err)
	}

	atomicTrieDB := memorydb.New()
	atomicTrie, err := NewBlockingAtomicTrie(atomicTrieDB, repo)
	atomicTrie.(*blockingAtomicTrie).commitHeightInterval = 10
	assert.NoError(t, err)
	dbCommitFn := func() error {
		return nil
	}

	err = atomicTrie.Initialize(lastAcceptedHeight, dbCommitFn)
	assert.NoError(t, err)

	lastCommittedHash, lastCommittedHeight, err := atomicTrie.LastCommitted()
	assert.NoError(t, err)
	assert.NotEqual(t, common.Hash{}, lastCommittedHash)
	assert.EqualValues(t, 1000, lastCommittedHeight, "expected %d but was %d", 1000, lastCommittedHeight)

	atomicTrie, err = NewBlockingAtomicTrie(atomicTrieDB, repo)
	assert.NoError(t, err)
	iterator, err := atomicTrie.Iterator(lastCommittedHash)
	assert.NoError(t, err)
	entriesIterated := uint64(0)
	for iterator.Next() {
		assert.Greater(t, len(iterator.AtomicOps()), 0)
		assert.Len(t, iterator.Errors(), 0)
		entriesIterated++
	}
	assert.Len(t, iterator.Errors(), 0)
	assert.EqualValues(t, 1001, entriesIterated, "expected %d was %d", 1001, entriesIterated)
}
