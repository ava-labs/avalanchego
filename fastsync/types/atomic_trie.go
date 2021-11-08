package types

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/fastsync/facades"
	"github.com/ethereum/go-ethereum/common"
)

// AtomicTrie defines an index containing the atomic operations
// in a syncable trie format
type AtomicTrie interface {
	// Initialize initializes the AtomicTrie from the last indexed
	// block to the last accepted block in the chain
	Initialize(chain facades.ChainFacade, dbCommitFn func() error, getAtomicTxFn func(blk facades.BlockFacade) (map[ids.ID]*atomic.Requests, error)) chan struct{}

	// Index indexes the given atomicOps at the specified block height
	// Returns an optional root hash and an optional error
	Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error)

	// Iterator returns an AtomicIterator to iterate the trie at the given
	// root hash
	// Optionally returns an error
	Iterator(hash common.Hash) (AtomicIterator, error)

	// Height returns the following:
	// - current index height
	// - whether the index is initialized
	// - optional error
	Height() (uint64, bool, error)

	// LastCommitted returns the following:
	// - last committed hash
	// - last committed block height
	// - optional error
	LastCommitted() (common.Hash, uint64, error)
}

// AtomicIterator defines a stateful iterator that iterates
// the AtomicTrie
type AtomicIterator interface {
	Next() bool
	BlockNumber() uint64
	BlockchainID() ids.ID
	Entries() *atomic.Requests
	Errors() []error
}
