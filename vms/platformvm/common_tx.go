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
	Ins []*ava.TransferableInput `serialize:"true"`

	// Output UTXOs
	Outs []*ava.TransferableOutput `serialize:"true"`
}
