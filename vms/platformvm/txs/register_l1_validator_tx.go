// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/types"
)

var _ UnsignedTx = (*RegisterL1ValidatorTx)(nil)

type RegisterL1ValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Balance <= sum($AVAX inputs) - sum($AVAX outputs) - TxFee.
	Balance uint64 `serialize:"true" json:"balance"`
	// ProofOfPossession of the BLS key that is included in the Message.
	ProofOfPossession [bls.SignatureLen]byte `serialize:"true" json:"proofOfPossession"`
	// Message is expected to be a signed Warp message containing an
	// AddressedCall payload with the RegisterL1Validator message.
	Message types.JSONByteSlice `serialize:"true" json:"message"`
}

func (tx *RegisterL1ValidatorTx) SyntacticVerify(ctx *snow.Context) error {
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

func (tx *RegisterL1ValidatorTx) Visit(visitor Visitor) error {
	return visitor.RegisterL1ValidatorTx(tx)
}
