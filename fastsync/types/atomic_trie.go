package types

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
)

// AtomicTrie defines an index containing the atomic operations
// in a syncable trie format
type AtomicTrie interface {
	// Initialize initializes the AtomicTrie from the last indexed
	// block to the last accepted block in the chain
	Initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error) error

	// Index indexes the given atomicOps at the specified block height
	// Returns an optional root hash and an optional error
	Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error)

	// Iterator returns an AtomicTrieIterator to iterate the trie at the given
	// root hash
	// Optionally returns an error
	Iterator(hash common.Hash) (AtomicTrieIterator, error)

	// LastCommitted returns the following:
	// - last committed hash
	// - last committed block height
	// - optional error
	LastCommitted() (common.Hash, uint64, error)

	// TrieDB returns the underlying trie database
	TrieDB() *trie.Database

	// Root returns the trie root if it exists at specified height with
	// an optional error
	Root(height uint64) (common.Hash, error)
}

// AtomicTrieIterator defines a stateful iterator that iterates
// the AtomicTrie
type AtomicTrieIterator interface {
	// Next returns the next node in the atomic trie that is being iterated
	Next() bool

	// BlockNumber returns the current block number
	BlockNumber() uint64

	// AtomicOps returns the current atomic requests mapped to the chain ID
	AtomicOps() map[ids.ID]*atomic.Requests

	// Errors returns list of errors, if any
	Errors() []error
}
