package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
)

// Max size of memo field
// Don't change without also changing avm.maxMemoSize
const maxMemoSize = 256

var (
	errBlockchainIDZero = errors.New("blockchain ID is empty")
	errVMNil            = errors.New("tx.vm is nil")
)

// BaseTx contains fields common to many transaction types
// Should be embedded in transaction implementations
// The serialized fields of this struct should be exactly the same as those of avm.BaseTx
// Do not change this struct's serialized fields without doing the same on avm.BaseTx
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
	NetworkID uint32 `serialize:"true"`
	// ID of this blockchain. In practice is always the empty ID.
	// This is only here to match avm.BaseTx's format
	BlockchainID ids.ID                    `serialize:"true"`
	Outputs      []*ava.TransferableOutput `serialize:"true"`
	// Input UTXOs
	Inputs []*ava.TransferableInput `serialize:"true"`
	// Memo field contains arbitrary bytes, up to maxMemoSize
	Memo []byte `serialize:"true"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *BaseTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// Bytes returns the byte representation of this tx
func (tx *BaseTx) Bytes() []byte {
	return tx.bytes
}

// ID returns this transaction's ID
func (tx *BaseTx) ID() ids.ID { return tx.id }

// Ins returns this transaction's inputs
func (tx *BaseTx) Ins() []*ava.TransferableInput { return tx.Inputs }

// Outs returns this transaction's outputs
func (tx *BaseTx) Outs() []*ava.TransferableOutput { return tx.Outputs }

// SyntacticVerify returns nil iff this tx is well formed
func (tx *BaseTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.vm == nil:
		return errVMNil
	case tx.BlockchainID.IsZero():
		return errBlockchainIDZero
	case len(tx.Memo) > maxMemoSize:
		return fmt.Errorf("memo length, %d, exceeds maximum memo length, %d", len(tx.Memo), maxMemoSize)

	}
	return nil
}
