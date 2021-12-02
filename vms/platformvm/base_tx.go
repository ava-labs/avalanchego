// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// BaseTx contains fields common to many transaction types. It should be
// embedded in transaction implementations.
type BaseTx struct {
	avax.BaseTx `serialize:"true" json:"inputs"`

	// true iff this transaction has already passed syntactic verification
	syntacticallyVerified bool
}

func (tx *BaseTx) InputIDs() ids.Set {
	inputIDs := ids.NewSet(len(tx.Ins))
	for _, in := range tx.Ins {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

// InitCtx sets the FxID fields in the inputs and outputs of this [BaseTx]. Also
// sets the [ctx] to the given [vm.ctx] so that the addresses can be json
// marshalled into human readable format
func (tx *BaseTx) InitCtx(ctx *snow.Context) {
	for _, in := range tx.BaseTx.Ins {
		in.FxID = secp256k1fx.ID
	}
	for _, out := range tx.BaseTx.Outs {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
}

// SyntacticVerify returns nil iff this tx is well formed
func (tx *BaseTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}
	if err := tx.MetadataVerify(ctx); err != nil {
		return fmt.Errorf("metadata failed verification: %w", err)
	}
	for _, out := range tx.Outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
	}
	for _, in := range tx.Ins {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("input failed verification: %w", err)
		}
	}
	switch {
	case !avax.IsSortedTransferableOutputs(tx.Outs, Codec):
		return errOutputsNotSorted
	case !avax.IsSortedAndUniqueTransferableInputs(tx.Ins):
		return errInputsNotSortedUnique
	default:
		return nil
	}
}
