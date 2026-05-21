// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/utils"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/vms/components/verify"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/signer"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/warp/message"
	"github.com/MetalBlockchain/metalgo/vms/secp256k1fx"
	"github.com/MetalBlockchain/metalgo/vms/types"
)

const MaxSubnetAddressLength = 4096

var (
	_ UnsignedTx                                  = (*ConvertSubnetToL1Tx)(nil)
	_ utils.Sortable[*ConvertSubnetToL1Validator] = (*ConvertSubnetToL1Validator)(nil)

	ErrConvertPermissionlessSubnet         = errors.New("cannot convert a permissionless subnet")
	ErrAddressTooLong                      = errors.New("address is too long")
	ErrConvertMustIncludeValidators        = errors.New("conversion must include at least one validator")
	ErrConvertValidatorsNotSortedAndUnique = errors.New("conversion validators must be sorted and unique")
	ErrZeroWeight                          = errors.New("validator weight must be non-zero")
)

type ConvertSubnetToL1Tx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the Subnet to transform
	Subnet ids.ID `serialize:"true" json:"subnetID"`
	// Chain where the Subnet manager lives
	ChainID ids.ID `serialize:"true" json:"chainID"`
	// Address of the Subnet manager
	Address types.JSONByteSlice `serialize:"true" json:"address"`
	// Initial pay-as-you-go validators for the Subnet
	Validators []*ConvertSubnetToL1Validator `serialize:"true" json:"validators"`
	// Authorizes this conversion
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

func (tx *ConvertSubnetToL1Tx) SyntacticVerify(ctx *snow.Context) error {
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
	case !utils.IsSortedAndUnique(tx.Validators):
		return ErrConvertValidatorsNotSortedAndUnique
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	for _, vdr := range tx.Validators {
		if err := vdr.Verify(); err != nil {
			return err
		}
	}
	if err := tx.SubnetAuth.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ConvertSubnetToL1Tx) Visit(visitor Visitor) error {
	return visitor.ConvertSubnetToL1Tx(tx)
}

type ConvertSubnetToL1Validator struct {
	// NodeID of this validator
	NodeID types.JSONByteSlice `serialize:"true" json:"nodeID"`
	// Weight of this validator used when sampling
	Weight uint64 `serialize:"true" json:"weight"`
	// Initial balance for this validator
	Balance uint64 `serialize:"true" json:"balance"`
	// [Signer] is the BLS key for this validator.
	// Note: We do not enforce that the BLS key is unique across all validators.
	//       This means that validators can share a key if they so choose.
	//       However, a NodeID + Subnet does uniquely map to a BLS key
	Signer signer.ProofOfPossession `serialize:"true" json:"signer"`
	// Leftover $AVAX from the [Balance] will be issued to this owner once it is
	// removed from the validator set.
	RemainingBalanceOwner message.PChainOwner `serialize:"true" json:"remainingBalanceOwner"`
	// This owner has the authority to manually deactivate this validator.
	DeactivationOwner message.PChainOwner `serialize:"true" json:"deactivationOwner"`
}

func (v *ConvertSubnetToL1Validator) Compare(o *ConvertSubnetToL1Validator) int {
	return bytes.Compare(v.NodeID, o.NodeID)
}

func (v *ConvertSubnetToL1Validator) Verify() error {
	if v.Weight == 0 {
		return ErrZeroWeight
	}
	nodeID, err := ids.ToNodeID(v.NodeID)
	if err != nil {
		return err
	}
	if nodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}
	return verify.All(
		&v.Signer,
		&secp256k1fx.OutputOwners{
			Threshold: v.RemainingBalanceOwner.Threshold,
			Addrs:     v.RemainingBalanceOwner.Addresses,
		},
		&secp256k1fx.OutputOwners{
			Threshold: v.DeactivationOwner.Threshold,
			Addrs:     v.DeactivationOwner.Addresses,
		},
	)
}
