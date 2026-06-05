// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/types"
)

var _ UnsignedTx = (*CreateL1Tx)(nil)

type CreateL1Tx struct {
	BaseTx `serialize:"true"`

	// Creating Chain Variables
	ChainName   string   `serialize:"true" json:"chainName"`
	VMID        ids.ID   `serialize:"true" json:"vmID"`
	FxIDs       []ids.ID `serialize:"true" json:"fxIDs"`
	GenesisData []byte   `serialize:"true" json:"genesisData"`

	// Validator Manager Variables
	ManagerChainID ids.ID              `serialize:"true" json:"chainID"`
	ManagerAddress types.JSONByteSlice `serialize:"true" json:"address"`

	// Initial Validators
	Validators []*ConvertSubnetToL1Validator `serialize:"true" json:"validators"`
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
	case len(tx.ChainName) > MaxNameLen:
		return errNameTooLong
	case len(tx.ManagerAddress) > MaxSubnetAddressLength:
		return ErrAddressTooLong
	case len(tx.Validators) == 0:
		return ErrConvertMustIncludeValidators
	case !utils.IsSortedAndUnique(tx.Validators):
		return ErrConvertValidatorsNotSortedAndUnique
	case !utils.IsSortedAndUnique(tx.FxIDs):
		return errFxIDsNotSortedAndUnique
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

// BlockchainID returns the blockchainID for the chain created by this tx.
// Defined as SHA256(subnetID || 0x00 || chainIndex), where chainIndex is 0
// since CreateL1Tx creates exactly one chain.
func (*CreateL1Tx) BlockchainID(subnetID ids.ID) ids.ID {
	packer := wrappers.Packer{Bytes: make([]byte, ids.IDLen+1+wrappers.IntLen)}
	packer.PackFixedBytes(subnetID[:])
	packer.PackByte(0x00)
	packer.PackInt(0)
	return hashing.ComputeHash256Array(packer.Bytes)
}

func (tx *CreateL1Tx) Visit(visitor Visitor) error {
	return visitor.CreateL1Tx(tx)
}
