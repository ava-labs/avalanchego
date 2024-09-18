// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/types"
)

const MaxSubnetAddressLength = 4096

var (
	_ UnsignedTx = (*ConvertSubnetTx)(nil)

	ErrConvertPermissionlessSubnet  = errors.New("cannot convert a permissionless subnet")
	ErrAddressTooLong               = errors.New("address is too long")
	ErrConvertMustIncludeValidators = errors.New("conversion must include at least one validator")
	ErrZeroWeight                   = errors.New("validator weight must be non-zero")
	ErrMissingPublicKey             = errors.New("missing public key")
)

type ConvertSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the Subnet to transform
	Subnet ids.ID `serialize:"true" json:"subnetID"`
	// Chain where the Subnet manager lives
	ChainID ids.ID `serialize:"true" json:"chainID"`
	// Address of the Subnet manager
	Address types.JSONByteSlice `serialize:"true" json:"address"`
	// Initial pay-as-you-go validators for the Subnet
	Validators []ConvertSubnetValidator `serialize:"true" json:"validators"`
	// Authorizes this conversion
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

func (tx *ConvertSubnetTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.Subnet == constants.PrimaryNetworkID:
		return ErrConvertPermissionlessSubnet
	case len(tx.Address) > MaxSubnetAddressLength:
		return ErrAddressTooLong
	case len(tx.Validators) == 0:
		return ErrConvertMustIncludeValidators
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	for _, vdr := range tx.Validators {
		if vdr.Weight == 0 {
			return ErrZeroWeight
		}
		if err := verify.All(vdr.Signer, vdr.RemainingBalanceOwner); err != nil {
			return err
		}
		if vdr.Signer.Key() == nil {
			return ErrMissingPublicKey
		}
	}
	if err := tx.SubnetAuth.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ConvertSubnetTx) Visit(visitor Visitor) error {
	return visitor.ConvertSubnetTx(tx)
}

type ConvertSubnetValidator struct {
	// TODO: Must be Ed25519 NodeID
	NodeID ids.NodeID `json:"nodeID"`
	// Weight of this validator used when sampling
	Weight uint64 `json:"weight"`
	// Initial balance for this validator
	Balance uint64 `json:"balance"`
	// [Signer] is the BLS key for this validator.
	// Note: We do not enforce that the BLS key is unique across all validators.
	//       This means that validators can share a key if they so choose.
	//       However, a NodeID + Subnet does uniquely map to a BLS key
	Signer signer.Signer `json:"signer"`
	// Leftover $AVAX from the [Balance] will be issued to this owner once it is
	// removed from the validator set.
	RemainingBalanceOwner fx.Owner `json:"remainingBalanceOwner"`
}
