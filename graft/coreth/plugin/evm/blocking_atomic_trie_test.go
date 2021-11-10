package evm

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/params"
	"github.com/stretchr/testify/assert"
)

type TestAtomicTx struct {
	avax.Metadata
	BlockchainID  ids.ID           `serialize:"true"`
	AtomicRequest *atomic.Requests `serialize:"true"`
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

func testDataImportTx() *TestAtomicTx {
	blockchainID := ids.GenerateTestID()
	return &TestAtomicTx{
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
	}
}

func testDataExportTx() *TestAtomicTx {
	blockchainID := ids.GenerateTestID()
	return &TestAtomicTx{
		BlockchainID: blockchainID,
		AtomicRequest: &atomic.Requests{
			RemoveRequests: [][]byte{
				utils.RandomBytes(32),
				utils.RandomBytes(32),
			},
		},
	}
}

func Test_BlockingAtomicTrie(t *testing.T) {
	db := memorydb.New()
	acceptedAtomicTxDB := memdb.New()

	codec := codec.NewDefaultManager()
	codecVersion := uint16(0)

	c := linearcodec.NewDefault()
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&TestAtomicTx{}),
		codec.RegisterCodec(codecVersion, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}

	tx := testDataImportTx()
	b, err := codec.Marshal(codecVersion, tx)
	assert.NoError(t, err)

	txBytes := make([]byte, wrappers.LongLen, wrappers.LongLen+len(b))
	binary.BigEndian.PutUint64(txBytes, 0)
	txBytes = append(txBytes, b...)

	txID := ids.GenerateTestID()
	err = acceptedAtomicTxDB.Put(txID[:], txBytes)
	assert.NoError(t, err)

	atomicTrie, err := NewBlockingAtomicTrie(db)
	assert.NoError(t, err)

	dbCommitFn := func() error {
		return nil
	}

	chainFacade := newTestChainFacade(newAtomicBlockFacade(0, common.Hash{}, nil), nil)
	doneChan := atomicTrie.Initialize(chainFacade, dbCommitFn, memdb.New().NewIterator(), Codec)
	_, open := <-doneChan
	assert.False(t, open)
	assert.NotNil(t, doneChan)
}
