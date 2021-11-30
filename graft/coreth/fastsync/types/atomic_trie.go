// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package types

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
)

// AtomicTrie maintains an index of atomic operations by chainID for every block height
// containing atomic transactions. The backing data structure for this index is a Trie.
// The keys of the trie represent the indexed keys (block heights), values (leaf nodes)
// representing the atomic operations.
type AtomicTrie interface {
	// Initialize initializes the AtomicTrie from the atomic repository ignoring
	// any atomic operations for any heights that have already been indexed
	Initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error) error

	// Index indexes the given atomicOps at the specified block height
	// Returns an optional root hash and an optional error
	// A non-empty root hash is returned when the atomic trie has been committed
	// Atomic trie is committed if the block height is within the commit interval
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

// AtomicTrieIterator is a stateful iterator that iterates leafs of an AtomicTrie
type AtomicTrieIterator interface {
	// Next returns the next node in the atomic trie that is being iterated
	Next() bool

	// BlockNumber returns the current block number
	BlockNumber() uint64

	// AtomicOps returns the current atomic requests mapped to the chain ID
	AtomicOps() map[ids.ID]*atomic.Requests

	// Error returns an error, if any
	Error() error
}
