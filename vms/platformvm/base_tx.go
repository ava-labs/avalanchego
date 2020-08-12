package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/avax"
)

// Max size of memo field
// Don't change without also changing avm.maxMemoSize
const maxMemoSize = 256

var (
	errVMNil             = errors.New("tx.vm is nil")
	errWrongBlockchainID = errors.New("wrong blockchain ID provided")
)

// BaseTx contains fields common to many transaction types. It should be
// embedded in transaction implementations. The serialized fields of this struct
// should be exactly the same as those of avm.BaseTx. Do not change this
// struct's serialized fields without doing the same on avm.BaseTx.
// TODO: Factor out this and avm.BaseTX
type BaseTx struct {
	vm *VM
	// true iff this transaction has already passed syntactic verification
	syntacticallyVerified bool
	// ID of this tx
	id ids.ID
	// Byte representation of this unsigned tx
	unsignedBytes []byte
	// Byte representation of the signed transaction (ie with credentials)
	bytes []byte

	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain. In practice is always the empty ID.
	// This is only here to match avm.BaseTx's format
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Output UTXOs
	Outs []*avax.TransferableOutput `serialize:"true" json:"outputs"`
	// Inputs consumed by this tx
	Ins []*avax.TransferableInput `serialize:"true" json:"inputs"`
	// Memo field contains arbitrary bytes, up to maxMemoSize
	Memo []byte `serialize:"true" json:"memo"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *BaseTx) UnsignedBytes() []byte { return tx.unsignedBytes }

// Bytes returns the byte representation of this tx
func (tx *BaseTx) Bytes() []byte { return tx.bytes }

// ID returns this transaction's ID
func (tx *BaseTx) ID() ids.ID { return tx.id }

// Verify returns nil iff this tx is well formed
func (tx *BaseTx) Verify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.id.IsZero():
		return errInvalidID
	case tx.vm == nil:
		return errVMNil
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return errWrongNetworkID
	case !tx.vm.Ctx.ChainID.Equals(tx.BlockchainID):
		return errWrongBlockchainID
	case len(tx.Memo) > maxMemoSize:
		return fmt.Errorf("memo length, %d, exceeds maximum memo length, %d",
			len(tx.Memo), maxMemoSize)
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
	case !avax.IsSortedTransferableOutputs(tx.Outs, Codec):
		return errOutputsNotSorted
	case !avax.IsSortedAndUniqueTransferableInputs(tx.Ins):
		return errInputsNotSortedUnique
	default:
		return nil
	}
}
