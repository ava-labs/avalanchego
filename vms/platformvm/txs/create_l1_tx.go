package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/types"
)

var (
	_ UnsignedTx = (*CreateL1Tx)(nil)
)

type CreateL1Tx struct {
	BaseTx `serialize:"true"`

	// Creating Chain Variables
	ChainName   string   `serialize:"true" json:"chainName"`
	VMID        ids.ID   `serialize:"true" json:"vmID"`
	FxIDs       []ids.ID `serialize:"true" json:"fxIDs"`
	GenesisData []byte   `serialize:"true" json:"genesisData"`

	// Validator manager
	ManagerChainID ids.ID              `serialize:"true" json:"chainID"`
	ManagerAddress types.JSONByteSlice `serialize:"true" json:"address"`

	// Initial validators
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

func (tx *CreateL1Tx) Visit(visitor Visitor) error {
	return visitor.CreateL1Tx(tx)
}
