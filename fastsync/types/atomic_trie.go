// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package types

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
)

// AtomicTrie maintains an index of atomic operations by blockchainIDs for every block
// height containing atomic transactions. The backing data structure for this index is
// a Trie. The keys of the trie are block heights and the values (leaf nodes)
// are the atomic operations applied to shared memory while processing the block accepted
// at the corresponding height.
type AtomicTrie interface {
	// Index indexes the given atomicOps at the specified block height
	// Returns an optional root hash
	// A non-empty root hash is returned when the atomic trie has been committed
	// Atomic trie is committed if the block height is within the commit interval
	Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error)

	// Iterator returns an AtomicTrieIterator to iterate the trie at the given
	// root hash
	Iterator(hash common.Hash, startHeight uint64) (AtomicTrieIterator, error)

	// LastCommitted returns the last committed hash and corresponding block height
	LastCommitted() (common.Hash, uint64)

	// TrieDB returns the underlying trie database
	TrieDB() *trie.Database

	// Root returns hash if it exists at specified height
	// if trie was not committed at provided height, it returns
	// common.Hash{} instead
	Root(height uint64) (common.Hash, error)
}

// AtomicTrieIterator is a stateful iterator that iterates the leafs of an AtomicTrie
type AtomicTrieIterator interface {
	// Next advances the iterator to the next node in the atomic trie and
	// returns true if there are more nodes to iterate
	Next() bool

	// BlockNumber returns the current block number
	BlockNumber() uint64

	// AtomicOps returns a map of blockchainIDs to the set of atomic requests
	// for that blockchainID at the current block number
	AtomicOps() map[ids.ID]*atomic.Requests

	// Error returns error, if any encountered during this iteration
	Error() error
}
