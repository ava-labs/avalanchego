package platformvm

import "github.com/ava-labs/gecko/ids"

// Metadata about a transaction
// Meant to be embedded in tx implementations
type Metadata struct {
	// ID of this tx
	id ids.ID

	// Byte representation of this unsigned tx
	unsignedBytes []byte

	// Byte representation of the signed transaction (ie with credentials)
	bytes []byte

	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true"`
}
