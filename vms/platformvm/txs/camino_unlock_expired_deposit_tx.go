// Copyright (C) 2022-2025, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ UnsignedTx = (*UnlockExpiredDepositTx)(nil)

// UnlockExpiredDepositTx is an unsigned UnlockExpiredDepositTx
type UnlockExpiredDepositTx struct {
	Ins  []*avax.TransferableInput  `serialize:"true" json:"inputs"`
	Outs []*avax.TransferableOutput `serialize:"true" json:"outputs"`

	unsignedBytes []byte // Unsigned byte representation of this data
}

func (*UnlockExpiredDepositTx) SyntacticVerify(_ *snow.Context) error {
	return nil
}

func (tx *UnlockExpiredDepositTx) SetBytes(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}

func (tx *UnlockExpiredDepositTx) Bytes() []byte {
	return tx.unsignedBytes
}

func (tx *UnlockExpiredDepositTx) InitCtx(ctx *snow.Context) {
	for _, in := range tx.Ins {
		in.FxID = secp256k1fx.ID
	}
	for _, out := range tx.Outs {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
}

func (tx *UnlockExpiredDepositTx) InputIDs() set.Set[ids.ID] {
	inputIDs := set.NewSet[ids.ID](len(tx.Ins))
	for _, in := range tx.Ins {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

func (tx *UnlockExpiredDepositTx) Outputs() []*avax.TransferableOutput {
	return tx.Outs
}

func (tx *UnlockExpiredDepositTx) Visit(visitor Visitor) error {
	return visitor.UnlockExpiredDepositTx(tx)
}
