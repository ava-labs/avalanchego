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
)

var _ state.Trie = (*reconstructedAccountTrie)(nil)

// reconstructedAccountTrie implements [state.Trie] backed by an [ffi.Reconstructed] view.
// Like [accountTrie], it accumulates BatchOps from writes. Unlike [accountTrie],
// Hash() chains Reconstruct() calls instead of creating proposals.
//
// Not concurrent-safe (matching Reconstructed's guarantees).
type reconstructedAccountTrie struct {
	baseAccountTrie
	reconstructed *ffi.Reconstructed
}

// newReconstructedAccountTrie creates a new reconstructed account trie.
// The caller transfers ownership of the Reconstructed handle to the trie;
// call Drop() on the trie when done.
func newReconstructedAccountTrie(recon *ffi.Reconstructed) (*reconstructedAccountTrie, error) {
	if recon == nil {
		return nil, errors.New("nil Reconstructed")
	}
	return &reconstructedAccountTrie{
		baseAccountTrie: baseAccountTrie{
			reader:    &reconstructedReader{reconstructed: recon},
			root:      common.Hash(recon.Root()),
			dirtyKeys: make(map[string][]byte),
		},
		reconstructed: recon,
	}, nil
}

// Drop releases the underlying Reconstructed handle.
func (a *reconstructedAccountTrie) Drop() {
	if a.reconstructed != nil {
		if err := a.reconstructed.Drop(); err != nil {
			log.Error("Failed to drop reconstructed trie", "error", err)
		}
		a.reconstructed = nil
	}
}

// Reconstructed returns the underlying Reconstructed handle.
// The caller must not Drop it; ownership remains with the trie.
func (a *reconstructedAccountTrie) Reconstructed() *ffi.Reconstructed {
	return a.reconstructed
}

// UpdateOps returns the accumulated batch operations since the last Hash() call.
func (a *reconstructedAccountTrie) UpdateOps() []ffi.BatchOp {
	return a.updateOps
}

// Hash computes the root hash by chaining Reconstruct() with accumulated ops.
// After this call, the internal Reconstructed is replaced with the new one,
// dirtyKeys are retained (they remain valid reads), and updateOps are cleared.
func (a *reconstructedAccountTrie) Hash() common.Hash {
	if !a.hasChanges {
		return a.root
	}

	// Reconstruct() invalidates the receiver and returns a new Reconstructed.
	// The old handle is consumed (no need to Drop it).
	newRecon, err := a.reconstructed.Reconstruct(a.updateOps)
	if err != nil {
		log.Error("Failed to reconstruct trie", "error", err)
		return common.Hash{}
	}

	a.reconstructed = newRecon
	a.reader = &reconstructedReader{reconstructed: newRecon}
	a.root = common.Hash(newRecon.Root())
	a.updateOps = nil
	a.hasChanges = false
	return a.root
}

// Commit returns the current root hash. No actual commit occurs since
// reconstruction is read-only and never persisted.
func (a *reconstructedAccountTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	hash := a.Hash()
	return hash, trienode.NewNodeSet(common.Hash{}), nil
}

// Copy creates a deep copy of the [reconstructedAccountTrie].
// The underlying Reconstructed and reader are shared, since they are read-only.
func (a *reconstructedAccountTrie) Copy() *reconstructedAccountTrie {
	return &reconstructedAccountTrie{
		baseAccountTrie: a.baseAccountTrie.copy(),
		reconstructed:   a.reconstructed, // shared, read-only
	}
}

// newReconstructedStorageTrie creates a [storageTrie] wrapping the
// given reconstructed account trie's base, for use in OpenStorageTrie.
func newReconstructedStorageTrie(acct *reconstructedAccountTrie) *storageTrie {
	return newStorageTrie(&acct.baseAccountTrie)
}
