package platformvm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
)

// CommonTx contains fields common to many transaction types
// Should be embedded in transaction implementations
type CommonTx struct {
	// ID of this tx
	id ids.ID

	// Byte representation of this unsigned tx
	unsignedBytes []byte

	// Byte representation of the signed transaction (ie with credentials)
	bytes []byte

	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true"`

	// Input UTXOs
	Inputs []*ava.TransferableInput `serialize:"true"`

	// Output UTXOs
	Outputs []*ava.TransferableOutput `serialize:"true"`
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
