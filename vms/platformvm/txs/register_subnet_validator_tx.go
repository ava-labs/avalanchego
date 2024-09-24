// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/types"
)

var _ UnsignedTx = (*RegisterSubnetValidatorTx)(nil)

type RegisterSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Balance <= sum($AVAX inputs) - sum($AVAX outputs) - TxFee.
	Balance uint64 `serialize:"true" json:"balance"`
	// [Signer] is the BLS key for this validator.
	// Note: We do not enforce that the BLS key is unique across all validators.
	//       This means that validators can share a key if they so choose.
	//       However, a NodeID does uniquely map to a BLS key
	Signer signer.Signer `serialize:"true" json:"signer"`
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
	if err := verify.All(tx.Signer, tx.RemainingBalanceOwner); err != nil {
		return err
	}
	if tx.Signer.Key() == nil {
		return ErrMissingPublicKey
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *RegisterSubnetValidatorTx) Visit(visitor Visitor) error {
	return visitor.RegisterSubnetValidatorTx(tx)
}
