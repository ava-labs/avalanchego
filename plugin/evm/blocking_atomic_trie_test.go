package evm

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

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
	AtomicRequests map[ids.ID]*atomic.Requests `serialize:"true"`
}

func (t *TestAtomicTx) Initialize(unsignedBytes, signedBytes []byte) {
	// no op
}

func (t *TestAtomicTx) ID() ids.ID {
	panic("implement me")
}

func (t *TestAtomicTx) GasUsed() (uint64, error) {
	panic("implement me")
}

func (t *TestAtomicTx) Burned(assetID ids.ID) (uint64, error) {
	panic("implement me")
}

func (t *TestAtomicTx) UnsignedBytes() []byte {
	panic("implement me")
}

func (t *TestAtomicTx) Bytes() []byte {
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
	return t.AtomicRequests, nil
}

func (t *TestAtomicTx) Accept(ctx *snow.Context, batch database.Batch) error {
	panic("implement me")
}

func (t *TestAtomicTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	panic("implement me")
}

// ignore
func Test_BlockingAtomicTrie(t *testing.T) {
	t.Skip()
	db := memorydb.New()
	acceptedAtomicTxDB := memdb.New()
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
		Codec.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}

	atomicRequests := make(map[ids.ID]*atomic.Requests)
	for j := 0; j < 3; j++ {
		blockchainID := ids.GenerateTestID()
		req := atomic.Requests{
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
		}
		atomicRequests[blockchainID] = &req
	}
	tx := &TestAtomicTx{AtomicRequests: atomicRequests}
	b, err := Codec.Marshal(codecVersion, tx)
	assert.NoError(t, err)

	txBytes := make([]byte, wrappers.LongLen, wrappers.LongLen+len(b))
	binary.BigEndian.PutUint64(txBytes, 0)
	txBytes = append(txBytes, b...)

	txID := ids.GenerateTestID()
	err = acceptedAtomicTxDB.Put(txID[:], txBytes)
	assert.NoError(t, err)

	atomicTrie, err := NewBlockingAtomicTrie(db, acceptedAtomicTxDB, Codec)
	assert.NoError(t, err)

	dbCommitFn := func() error {
		return nil
	}
	doneChan := atomicTrie.Initialize(testChainFacade{lastAcceptedBlock: newAtomicBlockFacade(0, common.Hash{}, nil)}, dbCommitFn, nil)
	_, open := <-doneChan
	assert.False(t, open)
	assert.NotNil(t, doneChan)
}
