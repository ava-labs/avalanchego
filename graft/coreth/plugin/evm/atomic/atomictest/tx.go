// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomictest

import (
	"math/rand"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

// TODO: Remove this and use actual codec and transactions (export, import)
var (
	_           atomic.UnsignedAtomicTx = (*TestUnsignedTx)(nil)
	TestTxCodec codec.Manager
)

func init() {
	TestTxCodec = codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&TestUnsignedTx{}),
		TestTxCodec.RegisterCodec(0, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

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
	VisitV                      error
	EVMStateTransferV           error
}

// GasUsed implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) GasUsed(bool) (uint64, error) { return t.GasUsedV, nil }

// Verify implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Verify(*snow.Context, extras.Rules) error { return t.VerifyV }

// AtomicOps implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) AtomicOps() (ids.ID, *avalancheatomic.Requests, error) {
	return t.AcceptRequestsBlockchainIDV, t.AcceptRequestsV, nil
}

// Initialize implements the UnsignedAtomicTx interface
func (*TestUnsignedTx) Initialize(_, _ []byte) {}

// ID implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) ID() ids.ID { return t.IDV }

// Burned implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Burned(ids.ID) (uint64, error) { return t.BurnedV, nil }

// Bytes implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Bytes() []byte { return t.UnsignedBytesV }

// SignedBytes implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) SignedBytes() []byte { return t.SignedBytesV }

// InputUTXOs implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) InputUTXOs() set.Set[ids.ID] { return t.InputUTXOsV }

// Visit implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) Visit(atomic.Visitor) error {
	return t.VisitV
}

// EVMStateTransfer implements the UnsignedAtomicTx interface
func (t *TestUnsignedTx) EVMStateTransfer(*snow.Context, atomic.StateDB) error {
	return t.EVMStateTransferV
}

var TestBlockchainID = ids.GenerateTestID()

func GenerateTestImportTxWithGas(gasUsed uint64, burned uint64) *atomic.Tx {
	return &atomic.Tx{
		UnsignedAtomicTx: &TestUnsignedTx{
			IDV:                         ids.GenerateTestID(),
			GasUsedV:                    gasUsed,
			BurnedV:                     burned,
			AcceptRequestsBlockchainIDV: TestBlockchainID,
			AcceptRequestsV: &avalancheatomic.Requests{
				RemoveRequests: [][]byte{
					utils.RandomBytes(32),
					utils.RandomBytes(32),
				},
			},
		},
	}
}

func GenerateTestImportTx() *atomic.Tx {
	return &atomic.Tx{
		UnsignedAtomicTx: &TestUnsignedTx{
			IDV:                         ids.GenerateTestID(),
			AcceptRequestsBlockchainIDV: TestBlockchainID,
			AcceptRequestsV: &avalancheatomic.Requests{
				RemoveRequests: [][]byte{
					utils.RandomBytes(32),
					utils.RandomBytes(32),
				},
			},
		},
	}
}

func GenerateTestExportTx() *atomic.Tx {
	return &atomic.Tx{
		UnsignedAtomicTx: &TestUnsignedTx{
			IDV:                         ids.GenerateTestID(),
			AcceptRequestsBlockchainIDV: TestBlockchainID,
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

func NewTestTx() *atomic.Tx {
	txType := rand.Intn(2)
	switch txType {
	case 0:
		return GenerateTestImportTx()
	case 1:
		return GenerateTestExportTx()
	default:
		panic("rng generated unexpected value for tx type")
	}
}

func NewTestTxs(numTxs int) []*atomic.Tx {
	txs := make([]*atomic.Tx, 0, numTxs)
	for i := 0; i < numTxs; i++ {
		txs = append(txs, NewTestTx())
	}

	return txs
}
