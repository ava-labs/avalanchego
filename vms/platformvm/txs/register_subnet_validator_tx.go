// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	_ UnsignedTx = (*RegisterSubnetValidatorTx)(nil)
)

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
	ChangeOwner fx.Owner `serialize:"true" json:"changeOwner"`
	// AddressedCall with Payload:
	//   - SubnetID
	//   - NodeID (must be Ed25519 NodeID)
	//   - Weight
	//   - BLS public key
	//   - Expiry
	Message warp.Message `serialize:"true" json:"message"`

	// true iff this transaction has already passed syntactic verification
	SyntacticallyVerified bool `json:"-"`

	// Populated during syntactic verification
	ParsedMessage *message.RegisterSubnetValidator `json:"-"`
}

func (tx *RegisterSubnetValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	if err := tx.Signer.Verify(); err != nil {
		return nil, false, err
	}
	key := tx.Signer.Key()
	return key, key != nil, nil
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *RegisterSubnetValidatorTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := tx.Signer.Verify(); err != nil {
		return fmt.Errorf("failed to verify signer: %w", err)
	}

	addressedCall, err := payload.ParseAddressedCall(tx.Message.Payload)
	if err != nil {
		return fmt.Errorf("failed to parse AddressedCall: %w", err)
	}

	msg, err := message.ParseRegisterSubnetValidator(addressedCall.Payload)
	if err != nil {
		return fmt.Errorf("failed to parse RegisterSubnetValidator: %w", err)
	}

	switch {
	case msg.SubnetID == constants.PrimaryNetworkID:
		return errors.New("cannot add Primary Network Validator")
	case msg.NodeID == ids.EmptyNodeID:
		return errors.New("cannot add EmptyNodeID")
	case msg.Weight == 0:
		return errors.New("cannot add Subnet Validator with weight of 0")
	case msg.Expiry == 0:
		return errors.New("cannot add Subnet Validator with expiry of 0")
	case len(msg.BlsPubKey) != bls.PublicKeyLen:
		return errors.New("invalid bls public key len")
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	tx.ParsedMessage = msg
	return nil
}

func (tx *RegisterSubnetValidatorTx) Visit(visitor Visitor) error {
	return visitor.RegisterSubnetValidatorTx(tx)
}
