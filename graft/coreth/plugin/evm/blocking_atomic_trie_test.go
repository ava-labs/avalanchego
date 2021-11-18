package evm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/fastsync/types"

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

const testCommitInterval = 100

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

func (tx *Tx) mustAtomicOps() map[ids.ID]*atomic.Requests {
	ops, err := tx.AtomicOps()
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

func Test_nearestCommitHeight(t *testing.T) {
	blockNumber := uint64(7029687)
	height := nearestCommitHeight(blockNumber, 4096)
	assert.Less(t, blockNumber, height+4096)
	fmt.Println(height)
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

	_, num, err := atomicTrie.LastCommitted()
	assert.NoError(t, err)
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

	_, num, err := atomicTrie.LastCommitted()
	assert.NoError(t, err)
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

func newTestAtomicTrieIndexer(t *testing.T) types.AtomicTrie {
	db := memdb.New()
	repo := newAtomicTxRepository(db, Codec)
	indexer, err := NewBlockingAtomicTrie(Database{db}, repo)
	assert.NoError(t, err)
	assert.NotNil(t, indexer)

	{
		// for test only to make it go faaaaaasst
		atomicTrieIndexer, ok := indexer.(*blockingAtomicTrie)
		assert.True(t, ok)
		atomicTrieIndexer.commitHeightInterval = testCommitInterval
	}

	return indexer
}

func Test_IndexerWriteAndRead(t *testing.T) {
	indexer := newTestAtomicTrieIndexer(t)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	var lastCommittedBlockHash common.Hash

	// process 205 blocks so that we get three commits (0, 100, 200)
	for height := uint64(0); height <= testCommitInterval*2+5; /*=205*/ height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight, err = indexer.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	// ensure we have 3 roots
	assert.Len(t, blockRootMap, 3)

	hash, height, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.EqualValues(t, lastCommittedBlockHeight, height, "expected %d was %d", 200, lastCommittedBlockHeight)
	assert.Equal(t, lastCommittedBlockHash, hash)

	// verify all roots are there
	for height, hash := range blockRootMap {
		root, err := indexer.Root(height)
		assert.NoError(t, err)
		assert.Equal(t, hash, root)
	}

	// ensure Index does not accept blocks older than last committed height
	_, err = indexer.Index(10, testDataExportTx().mustAtomicOps())
	assert.Error(t, err)
	assert.Equal(t, "height 10 must be after last committed height 200", err.Error())

	// ensure Index does not accept blocks beyond next committed height
	nextCommitHeight := lastCommittedBlockHeight + testCommitInterval + 1 // =301
	_, err = indexer.Index(nextCommitHeight, testDataExportTx().mustAtomicOps())
	assert.Error(t, err)
	assert.Equal(t, "height 301 not within the next commit height 300", err.Error())
}

func Test_IndexerInitializeFromState(t *testing.T) {
	indexer := newTestAtomicTrieIndexer(t)

	// ensure last is uninitialised
	hash, height, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), height)
	assert.Equal(t, common.Hash{}, hash)

	blockRootMap := make(map[uint64]common.Hash)
	// process blocks and insert them into the indexer
	for height := uint64(0); height <= testCommitInterval; height++ {
		atomicRequests := testDataImportTx().mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)

		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight, err := indexer.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	hash, height, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(testCommitInterval), height)
	assert.NotEqual(t, common.Hash{}, hash)

	blockAtomicOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	// process blocks and insert them into the indexer
	for height := uint64(testCommitInterval) + 1; height <= testCommitInterval*2; height++ {
		atomicRequests := testDataExportTx()
		blockAtomicOpsMap[height] = atomicRequests.mustAtomicOps()
		_, err := indexer.Index(height, atomicRequests.mustAtomicOps())
		assert.NoError(t, err)
	}

	hash, height, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(testCommitInterval)*2, height)
	assert.NotEqual(t, common.Hash{}, hash)
}
