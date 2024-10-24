// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/types"
)

var _ UnsignedTx = (*SetSubnetValidatorWeightTx)(nil)

type SetSubnetValidatorWeightTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Message is expected to be a signed Warp message containing an
	// AddressedCall payload with the SetSubnetValidatorWeight message.
	Message types.JSONByteSlice `serialize:"true" json:"message"`
}

func (tx *SetSubnetValidatorWeightTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *SetSubnetValidatorWeightTx) Visit(visitor Visitor) error {
	return visitor.SetSubnetValidatorWeightTx(tx)
}
