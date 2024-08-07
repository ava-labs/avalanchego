// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ UnsignedTx = (*CaminoRewardValidatorTx)(nil)

// CaminoRewardValidatorTx is a transaction that represents a proposal to
// remove a validator that is currently validating from the validator set.
//
// If this transaction is accepted and the next block accepted is a Commit
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX as well as a validating reward.
//
// If this transaction is accepted and the next block accepted is an Abort
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX but no reward.
type CaminoRewardValidatorTx struct {
	Ins  []*avax.TransferableInput  `serialize:"true" json:"inputs"`
	Outs []*avax.TransferableOutput `serialize:"true" json:"outputs"`

	RewardValidatorTx `serialize:"true"`
}

func (tx *CaminoRewardValidatorTx) InitCtx(ctx *snow.Context) {
	for _, in := range tx.Ins {
		in.FxID = secp256k1fx.ID
	}
	for _, out := range tx.Outs {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
}

func (tx *CaminoRewardValidatorTx) InputIDs() set.Set[ids.ID] {
	inputIDs := set.NewSet[ids.ID](len(tx.Ins))
	for _, in := range tx.Ins {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

func (tx *CaminoRewardValidatorTx) Outputs() []*avax.TransferableOutput {
	return tx.Outs
}
