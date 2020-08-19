package evm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/go-ethereum/common"
)

// Max size of memo field
// Don't change without also changing avm.maxMemoSize
const maxMemoSize = 256

var (
	errVMNil             = errors.New("tx.vm is nil")
	errWrongBlockchainID = errors.New("wrong blockchain ID provided")
	errWrongNetworkID    = errors.New("tx was issued with a different network ID")
	errNilTx             = errors.New("tx is nil")
	errInvalidID         = errors.New("invalid ID")
	errOutputsNotSorted  = errors.New("outputs not sorted")
)

type EVMOutput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
}

func (out *EVMOutput) Verify() error {
	return nil
}

// BaseTx contains fields common to many transaction types. It should be
// embedded in transaction implementations. The serialized fields of this struct
// should be exactly the same as those of avm.BaseTx. Do not change this
// struct's serialized fields without doing the same on avm.BaseTx.
// TODO: Factor out this and avm.BaseTX
type BaseTx struct {
	avax.Metadata
	// true iff this transaction has already passed syntactic verification
	syntacticallyVerified bool
	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain. In practice is always the empty ID.
	// This is only here to match avm.BaseTx's format
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Outputs
	Outs []EVMOutput `serialize:"true" json:"outputs"`
	// Inputs consumed by this tx
	Ins []*avax.TransferableInput `serialize:"true" json:"inputs"`
	// Memo field contains arbitrary bytes, up to maxMemoSize
	Memo []byte `serialize:"true" json:"memo"`
}

// Verify returns nil iff this tx is well formed
func (tx *BaseTx) Verify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.NetworkID != ctx.NetworkID:
		return errWrongNetworkID
	case !ctx.ChainID.Equals(tx.BlockchainID):
		return errWrongBlockchainID
	}
	for _, out := range tx.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
	}
	for _, in := range tx.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
	}
	switch {
	// TODO: check whether output addreses are sorted?
	case !avax.IsSortedAndUniqueTransferableInputs(tx.Ins):
		return errInputsNotSortedUnique
	default:
		return nil
	}
}
