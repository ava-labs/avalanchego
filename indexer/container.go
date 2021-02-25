package indexer

import "github.com/ava-labs/avalanchego/ids"

// Container ...
type Container struct {
	// ID of this container
	ID ids.ID `serialize:"true"`
	// Byte representation of this container
	Bytes []byte `serialize:"true"`
	// Index is the order in which this container was accepted
	Index uint64 `serialize:"true"`
	// Unix time at which this container was accepted
	Timestamp uint64 `serialize:"true"`
}
