// (c) 2020-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.
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
