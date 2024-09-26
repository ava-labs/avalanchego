// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var _ UnsignedTx = (*RegisterSubnetValidatorTx)(nil)

type RegisterSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Balance <= sum($AVAX inputs) - sum($AVAX outputs) - TxFee.
	Balance uint64 `serialize:"true" json:"balance"`
	// ProofOfPossession of the BLS key that is included in the Message.
	ProofOfPossession [bls.SignatureLen]byte `serialize:"true" json:"proofOfPossession"`
	// Leftover $AVAX from the Subnet Validator's Balance will be issued to
	// this owner after it is removed from the validator set.
	RemainingBalanceOwner fx.Owner `serialize:"true" json:"remainingBalanceOwner"`
	// AddressedCall with Payload:
	//   - SubnetID
	//   - NodeID (must be Ed25519 NodeID)
	//   - Weight
	//   - BLS public key
	//   - Expiry
	Message types.JSONByteSlice `serialize:"true" json:"message"`
}

func (tx *RegisterSubnetValidatorTx) SyntacticVerify(ctx *snow.Context) error {
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
	if err := tx.RemainingBalanceOwner.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *RegisterSubnetValidatorTx) Visit(visitor Visitor) error {
	return visitor.RegisterSubnetValidatorTx(tx)
}
