package platformvm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
)

// CommonTx contains fields common to many transaction types
// Should be embedded in transaction implementations
// The serialized fields of this struct should be exactly the same as those of avm.BaseTx
// Do not change this struct's serialized fields without doing the same on avm.BaseTx
// TODO: Factor out this and avm.BaseTX
type CommonTx struct {
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
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx CommonTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// Bytes returns the byte representation of this tx
func (tx CommonTx) Bytes() []byte {
	return tx.bytes
}

// ID returns this transaction's ID
func (tx CommonTx) ID() ids.ID { return tx.id }

// Ins returns this transaction's inputs
func (tx CommonTx) Ins() []*ava.TransferableInput { return tx.Inputs }

// Outs returns this transaction's outputs
func (tx CommonTx) Outs() []*ava.TransferableOutput { return tx.Outputs }
