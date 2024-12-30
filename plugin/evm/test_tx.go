// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"math/rand"

	"github.com/ava-labs/avalanchego/utils"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
)

type TestUnsignedTx struct {
	GasUsedV                    uint64                    `serialize:"true"`
	AcceptRequestsBlockchainIDV ids.ID                    `serialize:"true"`
	AcceptRequestsV             *avalancheatomic.Requests `serialize:"true"`
	VerifyV                     error
	IDV                         ids.ID `serialize:"true" json:"id"`
	BurnedV                     uint64 `serialize:"true"`
	UnsignedBytesV              []byte
	SignedBytesV                []byte
	InputUTXOsV                 set.Set[ids.ID]
	SemanticVerifyV             error
	EVMStateTransferV           error
}

var _ atomic.UnsignedAtomicTx = &TestUnsignedTx{}

// GasUsed implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) GasUsed(fixedFee bool) (uint64, error) { return t.GasUsedV, nil }

// Verify implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Verify(ctx *snow.Context, rules params.Rules) error { return t.VerifyV }

// AtomicOps implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) AtomicOps() (ids.ID, *avalancheatomic.Requests, error) {
	return t.AcceptRequestsBlockchainIDV, t.AcceptRequestsV, nil
}

// Initialize implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Initialize(unsignedBytes, signedBytes []byte) {}

// ID implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) ID() ids.ID { return t.IDV }

// Burned implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Burned(assetID ids.ID) (uint64, error) { return t.BurnedV, nil }

// Bytes implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Bytes() []byte { return t.UnsignedBytesV }

// SignedBytes implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) SignedBytes() []byte { return t.SignedBytesV }

// InputUTXOs implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) InputUTXOs() set.Set[ids.ID] { return t.InputUTXOsV }

// SemanticVerify implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) SemanticVerify(backend *atomic.Backend, stx *atomic.Tx, parent atomic.AtomicBlockContext, baseFee *big.Int) error {
	return t.SemanticVerifyV
}

// EVMStateTransfer implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) EVMStateTransfer(ctx *snow.Context, state atomic.StateDB) error {
	return t.EVMStateTransferV
}

func testTxCodec() codec.Manager {
	codec := codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&TestUnsignedTx{}),
		c.RegisterType(&avalancheatomic.Element{}),
		c.RegisterType(&avalancheatomic.Requests{}),
		codec.RegisterCodec(atomic.CodecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
	return codec
}

var blockChainID = ids.GenerateTestID()

func testDataImportTx() *atomic.Tx {
	return &atomic.Tx{
		UnsignedAtomicTx: &TestUnsignedTx{
			IDV:                         ids.GenerateTestID(),
			AcceptRequestsBlockchainIDV: blockChainID,
			AcceptRequestsV: &avalancheatomic.Requests{
				RemoveRequests: [][]byte{
					utils.RandomBytes(32),
					utils.RandomBytes(32),
				},
			},
		},
	}
}

func testDataExportTx() *atomic.Tx {
	return &atomic.Tx{
		UnsignedAtomicTx: &TestUnsignedTx{
			IDV:                         ids.GenerateTestID(),
			AcceptRequestsBlockchainIDV: blockChainID,
			AcceptRequestsV: &avalancheatomic.Requests{
				PutRequests: []*avalancheatomic.Element{
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
		},
	}
}

func newTestTx() *atomic.Tx {
	txType := rand.Intn(2)
	switch txType {
	case 0:
		return testDataImportTx()
	case 1:
		return testDataExportTx()
	default:
		panic("rng generated unexpected value for tx type")
	}
}

func newTestTxs(numTxs int) []*atomic.Tx {
	txs := make([]*atomic.Tx, 0, numTxs)
	for i := 0; i < numTxs; i++ {
		txs = append(txs, newTestTx())
	}

	return txs
}
