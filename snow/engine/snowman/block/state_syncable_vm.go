// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "github.com/ava-labs/avalanchego/ids"

type StateSyncableVM interface {
	// Enabled indicates whether the state sync is enabled for this VM
	Enabled() bool
	// ParseStateSummary returns a StateSummary from a given byte slice
	ParseStateSummary([]byte) StateSummary
	// StateSummary returns latest StateSummary with an optional error
	StateSummary() (StateSummary, error)
}

// StateSummary indicates summary of VM state at a specified position
// in the blockchain
type StateSummary interface {
	// Height returns block height in this state summary
	Height() uint64
	// Height returns block height in this state summary
	BlockID() ids.ID
	// StateRoot returns a byte slice representing the state root hash
	// that can be synced to
	StateRoot() []byte
	// AtomicStateRoot returns a byte slice representing the atomic state root
	// hash that can be synced to
	AtomicStateRoot() []byte
	// StartSync triggers a sync of this state summary to the VM
	StartSync(vm ChainVM) error
	// IsCompleted returns whether the state has been synced with an optional error
	// Returned error indicates an irrecoverable error encountered during sync
	IsCompleted() (bool, error)
	// Bytes returns the byte slice representation of this StateSummary object
	Bytes() []byte
}
