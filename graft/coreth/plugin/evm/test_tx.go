// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.
package evm

import (
	"math/big"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"
)

type TestTx struct {
	GasUsedV          uint64
	AcceptRequestsV   *atomic.Requests
	VerifyV           error
	IDV               ids.ID `serialize:"true" json:"id"`
	BurnedV           uint64
	UnsignedBytesV    []byte
	BytesV            []byte
	InputUTXOsV       ids.Set
	SemanticVerifyV   error
	EVMStateTransferV error
	AtomicOpsV        map[ids.ID]*atomic.Requests
}

var _ UnsignedAtomicTx = &TestTx{}

// GasUsed implements the UnsignedAtomicTx interface
func (t *TestTx) GasUsed(fixedFee bool) (uint64, error) { return t.GasUsedV, nil }

// Verify implements the UnsignedAtomicTx interface
func (t *TestTx) Verify(ctx *snow.Context, rules params.Rules) error { return t.VerifyV }

// Accept implements the UnsignedAtomicTx interface
func (t *TestTx) Accept() (ids.ID, *atomic.Requests, error) {
	return t.IDV, t.AcceptRequestsV, nil
}

// Accept implements the UnsignedAtomicTx interface
func (t *TestTx) Initialize(unsignedBytes, signedBytes []byte) {}

// ID implements the UnsignedAtomicTx interface
func (t *TestTx) ID() ids.ID { return t.IDV }

// Burned implements the UnsignedAtomicTx interface
func (t *TestTx) Burned(assetID ids.ID) (uint64, error) { return t.BurnedV, nil }

// UnsignedBytes implements the UnsignedAtomicTx interface
func (t *TestTx) UnsignedBytes() []byte { return t.UnsignedBytesV }

// Bytes implements the UnsignedAtomicTx interface
func (t *TestTx) Bytes() []byte { return t.BytesV }

// ByInputUTXOs implements the UnsignedAtomicTx interface
func (t *TestTx) InputUTXOs() ids.Set { return t.InputUTXOsV }

// SemanticVerify implements the UnsignedAtomicTx interface
func (t *TestTx) SemanticVerify(vm *VM, stx *Tx, parent *Block, baseFee *big.Int, rules params.Rules) error {
	return t.SemanticVerifyV
}

// AtomicOps implements the UnsignedAtomicTx interface
func (t *TestTx) AtomicOps() (map[ids.ID]*atomic.Requests, error) { return t.AtomicOpsV, nil }

// EVMStateTransfer implements the UnsignedAtomicTx interface
func (t *TestTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	return t.EVMStateTransferV
}
