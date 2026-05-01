// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb/database"
)

var (
	_ state.Trie = (*reconstructedAccountTrie)(nil)

	errNilReconstructed = errors.New("nil Reconstructed")
)

// reconstructedAccountTrie implements [state.Trie] backed by an [ffi.Reconstructed] view.
// Like [accountTrie], it accumulates BatchOps from writes. Unlike [accountTrie],
// Hash() chains Reconstruct() calls instead of creating proposals.
//
// If computeRootOnHash is true, Hash and Commit apply pending writes and then
// compute the updated reconstructed root. Otherwise, they apply pending writes
// but return the previously cached root, avoiding the expensive root computation
// when the caller only needs Hash as a state-flush point and validates the final
// root separately.
//
// Not concurrent-safe (matching Reconstructed's guarantees).
type reconstructedAccountTrie struct {
	baseTrie
	recon             *ffi.Reconstructed
	computeRootOnHash bool
}

// newReconstructedAccountTrie creates a new reconstructed account trie.
// The caller retains ownership of the [ffi.Reconstructed] handle and must ensure
// it outlives the trie.
// computeRootOnHash controls whether Hash and Commit update the cached root.
func newReconstructedAccountTrie(recon *ffi.Reconstructed, computeRootOnHash bool) (*reconstructedAccountTrie, error) {
	if recon == nil {
		return nil, errNilReconstructed
	}
	return &reconstructedAccountTrie{
		baseTrie: baseTrie{
			reader:    &reconstructedReader{reconstructed: recon},
			root:      common.Hash(recon.Root()),
			dirtyKeys: make(map[string][]byte),
		},
		recon:             recon,
		computeRootOnHash: computeRootOnHash,
	}, nil
}

// Hash returns the current hash of the reconstructed trie.
// This will chain Reconstruct() with the accumulated ops. If computeRootOnHash
// is false, it applies the ops but returns the cached root without computing the
// reconstructed root.
// If there are no changes since the last call, the cached root is returned.
// On error, the zero hash is returned.
func (r *reconstructedAccountTrie) Hash() common.Hash {
	hash, err := r.hash()
	if err != nil {
		log.Error("Failed to hash reconstructed trie", "error", err)
		return common.Hash{}
	}
	return hash
}

func (r *reconstructedAccountTrie) hash() (common.Hash, error) {
	if r.hasChanges {
		// Reconstruct() mutates the receiver in place with the new state.
		if err := r.recon.Reconstruct(r.updateOps); err != nil {
			return common.Hash{}, err
		}
		if r.computeRootOnHash {
			r.root = common.Hash(r.recon.Root())
		}
		// Unlike accountTrie, updateOps must be cleared because Reconstruct()
		// is incremental (mutates in place), whereas createProposals() replays
		// all ops from the parent root each time.
		r.updateOps = nil
		r.dirtyKeys = make(map[string][]byte)
		r.hasChanges = false
	}
	return r.root, nil
}

// Commit returns the new root hash of the trie and an empty [trienode.NodeSet].
// No persistence occurs; reconstructed views exist only in memory and are not
// committed to the Firewood database.
func (r *reconstructedAccountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	hash, err := r.hash()
	if err != nil {
		return common.Hash{}, nil, err
	}
	return hash, trienode.NewNodeSet(common.Hash{}), nil
}

var _ database.Reader = (*reconstructedReader)(nil)

// reconstructedReader adapts an [ffi.Reconstructed] to the [database.Reader] interface.
// The underlying [ffi.Reconstructed] may be mutated by Reconstruct() calls, which
// changes what Get() returns.
type reconstructedReader struct {
	reconstructed *ffi.Reconstructed
}

// Node retrieves the value at the given path from the reconstructed view.
func (r *reconstructedReader) Node(_ common.Hash, path []byte, _ common.Hash) ([]byte, error) {
	return r.reconstructed.Get(path)
}
