// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var (
	_ UnsignedTx = (*CreateL1Tx)(nil)

	SelfManagerChainID = ids.ID{'m', 'a', 'n', 'a', 'g', 'e', 'r', ' ', 'o', 'n', ' ', 's', 'e', 'l', 'f'}
)

type CreateL1Tx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the VM running on the chain
	VMID ids.ID `serialize:"true" json:"vmID"`

	// Byte representation of genesis state of the chain
	GenesisData []byte `serialize:"true" json:"genesisData"`

	// Chain where the L1 validator manager lives.
	// it can be sekfManagerChainID if the validator manager lives on the same chain created by this tx.
	ManagerChainID ids.ID `serialize:"true" json:"chainID"`

	// Address of the L1 validator manager
	ManagerAddress types.JSONByteSlice `serialize:"true" json:"address"`

	// Initial pay-as-you-go validators for the L1
	Validators []*CreateL1Validator `serialize:"true" json:"validators"`
}

func (tx *CreateL1Tx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.VMID == ids.Empty:
		return errInvalidVMID
	case len(tx.ManagerAddress) > MaxSubnetAddressLength:
		return ErrAddressTooLong
	case tx.ManagerChainID == ids.Empty:
		return errEmptyManagerChainID
	case len(tx.Validators) == 0:
		return ErrConvertMustIncludeValidators
	case !utils.IsSortedAndUnique(tx.Validators):
		return ErrConvertValidatorsNotSortedAndUnique
	case len(tx.GenesisData) > MaxGenesisLen:
		return errGenesisTooLong
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	for _, vdr := range tx.Validators {
		if err := vdr.Verify(); err != nil {
			return err
		}
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *CreateL1Tx) Visit(visitor Visitor) error {
	return visitor.CreateL1Tx(tx)
}

type CreateL1Validator struct {
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

func (v *CreateL1Validator) Compare(o *CreateL1Validator) int {
	return bytes.Compare(v.NodeID, o.NodeID)
}

func (v *CreateL1Validator) Verify() error {
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
